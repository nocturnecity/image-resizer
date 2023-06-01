package pkg

type ResultSize struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type Size struct {
	SizeName         string            `json:"size_name"`
	ResizeOptions    *ResizeOptions    `json:"resize_options"`
	CropOptions      *CropOptions      `json:"crop_options"`
	WaterMarkOptions *WaterMarkOptions `json:"water_mark_options"`
	KeepFormat       bool              `json:"keep_format"`
}

type ResizeOptions struct {
	X            uint `json:"x"`
	Y            uint `json:"y"`
	QuickResize  bool `json:"quick_resize"`
	ImageQuality int  `json:"image_quality"`
}

type CropOptions struct {
	Width  uint `json:"width"`
	Height uint `json:"height"`
	X      int  `json:"x"`
	Y      int  `json:"y"`
}

type WaterMarkOptions struct {
	Width  uint `json:"width"`
	Height uint `json:"height"`
	X      int  `json:"x"`
	Y      int  `json:"y"`
}
