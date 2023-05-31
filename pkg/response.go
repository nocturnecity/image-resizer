package pkg

type Response struct {
	Sizes map[string]ResultSize `json:"sizes"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
