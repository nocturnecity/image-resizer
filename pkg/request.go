package pkg

import "fmt"

type Request struct {
	OriginalPath string `json:"original_path"`
	PathToSave   string `json:"path_to_save"`
	Format       string `json:"format"`
	BucketName   string `json:"bucket_name"`
	Sizes        []Size `json:"sizes"`
}

func (req *Request) Validate() error {
	if req.Format == "" {
		return fmt.Errorf("format is requered field")
	}

	if req.BucketName == "" {
		return fmt.Errorf("bucket_name is requered field")
	}

	if req.PathToSave == "" {
		return fmt.Errorf("path_to_save is requered field")
	}

	if req.OriginalPath == "" {
		return fmt.Errorf("original_path is requered field")
	}

	if req.PathToSave == "" {
		return fmt.Errorf("path_to_save is requered field")
	}

	if len(req.Sizes) <= 0 {
		return fmt.Errorf("at least 1 size required")
	}

	for i, size := range req.Sizes {
		if size.SizeName == "" {
			return fmt.Errorf("sizes[%d].size_name is required field", i)
		}

		if size.ResizeOptions == nil {
			return fmt.Errorf("sizes[%d].resize_options is required field", i)
		}
	}

	return nil
}
