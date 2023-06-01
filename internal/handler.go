package internal

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"

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

func NewResizeHandler(request pkg.Request, stdLog *StdLog) *ResizeHandler {
	return &ResizeHandler{
		Request:      request,
		log:          stdLog,
		cleanUpFiles: []string{},
	}
}

type ResizeHandler struct {
	Request         pkg.Request
	log             *StdLog
	cleanUpFiles    []string
	cleanUpAwsFiles []string
	session         *session.Session
}

func (rh *ResizeHandler) ProcessRequest() (map[string]pkg.ResultSize, error) {
	defer rh.cleanup()
	rh.log.Debug("Processing request %v", rh.Request)
	originalFileName := rh.generateRandomFileName(rh.Request.Format)
	err := rh.downloadFromS3(rh.Request.BucketName, rh.Request.OriginalPath, originalFileName, rh.Request.Region)
	if err != nil {
		return nil, fmt.Errorf("process request error: %w", err)
	}
	err = rh.stripAndRotateOriginal(originalFileName, originalFileName)
	if err != nil {
		return nil, fmt.Errorf("process request error: %w", err)
	}
	result := map[string]pkg.ResultSize{}
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
		err = rh.uploadToS3(rh.Request.BucketName, path, toSave, rh.Request.Region)
		if err != nil {
			return nil, fmt.Errorf("process request error: %w", err)
		}
		originalFileName = newOriginal
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
		err = rh.resizeCommand(finalFileName, cropFileName, size.ResizeOptions)
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
	rh.cleanUpFiles = append(rh.cleanUpFiles, filename)

	return filename
}

func (rh *ResizeHandler) cleanup() {
	for _, toDelete := range rh.cleanUpFiles {
		err := os.Remove(toDelete)
		if err != nil {
			rh.log.Error("error clean up file delete: %v", err)
		}
	}
}

func (rh *ResizeHandler) CleanupOnError() {
	// todo cleanup uploaded files
}

func (rh *ResizeHandler) getSortSizes() []pkg.Size {
	return rh.Request.Sizes // todo sort sizes
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

func (rh *ResizeHandler) uploadToS3(filename, bucketName, path string, region string) error {
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
		Bucket: aws.String(bucketName),
		Key:    aws.String(path),
		Body:   file,
	})
	if err != nil {
		return fmt.Errorf("failed to upload file, %v", err)
	}

	rh.log.Debug("Put file to S3 %s", path)
	rh.cleanUpAwsFiles = append(rh.cleanUpAwsFiles, path)
	return nil
}

func (rh *ResizeHandler) stripAndRotateOriginal(filename, result string) error {
	cmd := exec.Command("convert", filename, "-auto-orient", "-strip", result)
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
