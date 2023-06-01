package internal

import (
	"bufio"
	"fmt"
	"giggster.com/resizer/pkg"
	"github.com/google/uuid"
	"os"
	"os/exec"
	"strconv"
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
}

func (rh *ResizeHandler) ProcessRequest() (map[string]pkg.ResultSize, error) {
	defer rh.cleanup()
	rh.log.Debug("Processing request %v", rh.Request)
	originalFileName := rh.generateRandomFileName(rh.Request.Format)
	err := rh.getFileFromAWS(rh.Request.BucketName, rh.Request.OriginalPath, originalFileName)
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
		err = rh.putFileToAWS(rh.Request.BucketName, path, toSave)
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

func (rh *ResizeHandler) getFileFromAWS(bucketName, path, result string) error {
	cmd := exec.Command("aws", "s3", "cp", fmt.Sprintf("s3://%s/%s", bucketName, path), result)
	rh.log.Debug(cmd.String())
	res, err := cmd.CombinedOutput()
	if string(res) != "" {
		rh.log.Debug(string(res))
	}
	if err != nil {
		return fmt.Errorf("error get file from S3 %w", err)
	}
	rh.log.Debug("Receive file from S3 %s", path)
	return nil
}

func (rh *ResizeHandler) putFileToAWS(bucketName, path, filename string) error {
	cmd := exec.Command("aws", "s3", "cp", filename, fmt.Sprintf("s3://%s/%s", bucketName, path))
	rh.log.Debug(cmd.String())
	res, err := cmd.CombinedOutput()
	if string(res) != "" {
		rh.log.Debug(string(res))
	}
	if err != nil {
		return fmt.Errorf("error put file to S3 %w", err)
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
