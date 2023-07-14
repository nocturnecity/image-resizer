package internal

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/google/uuid"
	"github.com/nocturnecity/image-resizer/pkg"
)

const DefaultWatermarkPath = "watermark@2x.png"
const DefaultJpegFormat = "jpeg"
const DefaultWatermarkFormat = "png"
const DefaultWatermarkQuality = 100
const DefaultWatermarkDissolve = "100"
const DefaultResizerFilter = "Lanczos2"

func NewResizeHandler(request pkg.Request, stdLog *StdLog) *ResizeHandler {
	return &ResizeHandler{
		Request:         request,
		log:             stdLog,
		cleanUpFiles:    sync.Map{},
		cleanUpAwsFiles: sync.Map{},
	}
}

type ResizeHandler struct {
	Request         pkg.Request
	log             *StdLog
	cleanUpFiles    sync.Map
	cleanUpAwsFiles sync.Map
	session         *session.Session
}

func (rh *ResizeHandler) ProcessRequest() (map[string]pkg.ResultSize, error) {
	rh.log.Debug("Processing request %v", rh.Request)
	originalFileName := rh.generateRandomFileName(rh.Request.Format)
	err := rh.downloadFromS3(rh.Request.BucketName, rh.Request.OriginalPath, originalFileName, rh.Request.Region)
	if err != nil {
		return nil, fmt.Errorf("process request error: %w", err)
	}
	rh.log.Debug("RESIZE STARTED for: %s", rh.Request.OriginalPath)
	err = rh.stripAndRotateOriginal(originalFileName, originalFileName)
	if err != nil {
		return nil, fmt.Errorf("process request error: %w", err)
	}
	result := map[string]pkg.ResultSize{}
	var wg sync.WaitGroup
	hassUploadError := false
	wg.Add(len(rh.Request.Sizes))
	for _, size := range rh.getSortSizes() {
		format := rh.Request.Format
		if !size.KeepFormat {
			format = DefaultJpegFormat
		}
		toSave, newOriginal, err := rh.processSize(originalFileName, format, size)
		if err != nil {
			return nil, fmt.Errorf("process request error: %w", err)
		}
		path := fmt.Sprintf("%s/%s.%s", rh.Request.PathToSave, size.SizeName, format)
		info, err := rh.getResultFileInfo(toSave, path)
		if err != nil {
			return nil, fmt.Errorf("process request error: %w", err)
		}
		result[size.SizeName] = *info
		originalFileName = newOriginal
		go func() {
			defer wg.Done()
			err = rh.uploadToS3(rh.Request.BucketName, path, toSave, rh.Request.Region)
			if err != nil {
				hassUploadError = true
				rh.log.Error("process request error: %w", err)
			}
		}()
	}
	wg.Wait()
	rh.log.Debug("RESIZE COMPLETED for: %s", rh.Request.OriginalPath)
	if hassUploadError {
		return nil, fmt.Errorf("process request error: files failed to uploade to S3")
	}

	return result, nil
}

func (rh *ResizeHandler) processSize(originalFilename, format string, size pkg.Size) (string, string, error) {
	resizedFileName := rh.generateRandomFileName(format)
	err := rh.resizeCommand(originalFilename, resizedFileName, size.ResizeOptions)
	if err != nil {
		return "", "", err
	}
	finalFileName := resizedFileName

	if size.CropOptions != nil {
		cropFileName := rh.generateRandomFileName(format)
		err = rh.cropCommand(finalFileName, cropFileName, size.CropOptions)
		if err != nil {
			return "", "", err
		}
		finalFileName = cropFileName
	}

	if size.WaterMarkOptions != nil {
		watermarkedFileName := rh.generateRandomFileName(format)
		err := rh.waterMarkCommand(finalFileName, watermarkedFileName, size.WaterMarkOptions)
		if err != nil {
			return "", "", err
		}
		finalFileName = watermarkedFileName
	}

	return finalFileName, resizedFileName, nil
}

func (rh *ResizeHandler) generateRandomFileName(format string) string {
	filename := fmt.Sprintf("%s.%s", uuid.New(), format)
	rh.cleanUpFiles.Store(filename, filename)

	return filename
}

func (rh *ResizeHandler) Cleanup() {
	rh.cleanUpFiles.Range(func(_, value any) bool {
		toDelete := value.(string)
		err := os.Remove(toDelete)
		if err != nil {
			rh.log.Error("error clean up file delete: %v", err)
		}
		return true
	})
}

func (rh *ResizeHandler) CleanupOnError() {
	var err error
	if rh.session == nil {
		rh.session, err = session.NewSession(&aws.Config{
			Region: aws.String(rh.Request.Region),
		})
		if err != nil {
			rh.log.Error("failed to create session: %v", err)
			return
		}
	}
	s3Client := s3.New(rh.session)
	rh.cleanUpAwsFiles.Range(func(_, value any) bool {
		toDelete := value.(string)
		_, err = s3Client.DeleteObject(
			&s3.DeleteObjectInput{
				Bucket: aws.String(rh.Request.BucketName),
				Key:    aws.String(toDelete),
			})
		if err != nil {
			rh.log.Error("failed to delete on error: %v", err)
		}
		return true
	})
}

func (rh *ResizeHandler) getSortSizes() []pkg.Size {
	sort.Sort(sortBySize(rh.Request.Sizes))
	return rh.Request.Sizes
}

type sortBySize []pkg.Size

func (a sortBySize) Len() int {
	return len(a)
}
func (a sortBySize) Swap(i, j int) {
	a[i],
		a[j] = a[j],
		a[i]
}
func (a sortBySize) Less(i, j int) bool {
	return (a[i].ResizeOptions.X > a[j].ResizeOptions.X) && (a[i].ResizeOptions.Y >= a[j].ResizeOptions.Y)
}

func (rh *ResizeHandler) downloadFromS3(bucketName, path, result string, region string) error {
	// Create a new AWS session
	var err error
	if rh.session == nil {
		rh.session, err = session.NewSession(&aws.Config{
			Region: aws.String(region), // replace with your desired region
		})
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}
	}
	// Create a new S3 manager
	downloader := s3manager.NewDownloader(rh.session)

	// Open a file for writing
	file, err := os.Create(result)
	if err != nil {
		return fmt.Errorf("failed to create file %q, %v", result, err)
	}

	// Download the object using the S3 manager
	_, err = downloader.Download(file,
		&s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(path),
		})
	if err != nil {
		return fmt.Errorf("failed to download file, %v", err)
	}

	rh.log.Debug("Receive file from S3 %s", path)
	return nil
}

func (rh *ResizeHandler) uploadToS3(bucketName, path, filename string, region string) error {
	// Create a new AWS session
	// Create a new AWS session
	var err error
	if rh.session == nil {
		rh.session, err = session.NewSession(&aws.Config{
			Region: aws.String(region), // replace with your desired region
		})
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}
	}
	// Create a new S3 uploader
	uploader := s3manager.NewUploader(rh.session)

	// Open the file for reading
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open file %q, %v", filename, err)
	}

	// Upload the file to S3
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(path),
		Body:        file,
		ContentType: aws.String("image/jpeg"),
		// TODO: fix it changing Cloudfront settings
		ACL: aws.String("public-read"),
	})
	if err != nil {
		return fmt.Errorf("failed to upload file, %v", err)
	}

	rh.log.Debug("Put file to S3 %s", path)
	rh.cleanUpAwsFiles.Store(path, path)
	return nil
}

func (rh *ResizeHandler) stripAndRotateOriginal(filename, result string) error {
	cmd := exec.Command(
		"convert",
		filename,
		"-auto-orient",
		"-strip",
		result)
	rh.log.Debug(cmd.String())
	res, err := cmd.CombinedOutput()
	if string(res) != "" {
		rh.log.Debug(string(res))
	}
	if err != nil {
		return fmt.Errorf("error strip original %w", err)
	}

	return nil
}

func (rh *ResizeHandler) resizeCommand(filename, result string, opt *pkg.ResizeOptions) error {
	cmd := exec.Command(
		"convert",
		filename,
		"-resize",
		fmt.Sprintf("%dx%d", opt.X, opt.Y),
		"-filter",
		DefaultResizerFilter,
		"-quality",
		fmt.Sprintf("%d", opt.ImageQuality),
		result)
	if opt.QuickResize {
		cmd = exec.Command(
			"convert",
			filename,
			"-scale",
			fmt.Sprintf("%dx%d", opt.X, opt.Y),
			"-quality",
			fmt.Sprintf("%d", opt.ImageQuality),
			result)
	}
	rh.log.Debug(cmd.String())
	res, err := cmd.CombinedOutput()
	if string(res) != "" {
		rh.log.Debug(string(res))
	}
	if err != nil {
		return fmt.Errorf("error resize file %w", err)

	}

	return nil
}

func (rh *ResizeHandler) cropCommand(filename, result string, opt *pkg.CropOptions) error {
	cmd := exec.Command(
		"convert",
		filename,
		"-crop",
		fmt.Sprintf("%dx%d+%d+%d", opt.Width, opt.Height, opt.X, opt.Y),
		result)
	rh.log.Debug(cmd.String())
	res, err := cmd.CombinedOutput()
	if string(res) != "" {
		rh.log.Debug(string(res))
	}
	if err != nil {
		return fmt.Errorf("error crop file %w", err)
	}

	return nil
}

func (rh *ResizeHandler) waterMarkCommand(filename, result string, opt *pkg.WaterMarkOptions) error {
	watermarkImage := rh.generateRandomFileName(DefaultWatermarkFormat)
	err := rh.resizeCommand(DefaultWatermarkPath, watermarkImage, &pkg.ResizeOptions{
		ImageQuality: DefaultWatermarkQuality,
		QuickResize:  false,
		X:            opt.Width,
		Y:            opt.Height,
	})
	if err != nil {
		return fmt.Errorf("error add watermark to file %w", err)
	}
	cmd := exec.Command(
		"composite",
		"-dissolve",
		DefaultWatermarkDissolve,
		"-gravity",
		"northwest",
		"-geometry",
		fmt.Sprintf("+%d+%d", opt.X, opt.Y),
		watermarkImage,
		filename,
		result)
	rh.log.Debug(cmd.String())
	res, err := cmd.CombinedOutput()
	if string(res) != "" {
		rh.log.Debug(string(res))
	}
	if err != nil {
		return fmt.Errorf("error add watermark to file %w", err)
	}

	return nil
}

func (rh *ResizeHandler) getResultFileInfo(filename, path string) (*pkg.ResultSize, error) {
	cmd := exec.Command("identify", "-format", "%w\n%h", filename)
	rh.log.Debug(cmd.String())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Split(bufio.ScanLines)

	imageInfo := &pkg.ResultSize{
		Path: path,
	}

	i := 0
	for scanner.Scan() {
		line := scanner.Text()
		switch i {
		case 0:
			imageInfo.Width, _ = strconv.Atoi(line)
		case 1:
			imageInfo.Height, _ = strconv.Atoi(line)
		}
		i++
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	return imageInfo, nil
}
