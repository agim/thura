package collabora

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"thura/internal/platform/servicehttp"
	"time"
)

// Convert uploads only the authorized source bytes to the fixed private service.
func (c Config) Convert(ctx context.Context, name, target string, data []byte) ([]byte, error) {
	if target != "docx" && target != "xlsx" && target != "pptx" {
		return nil, errors.New("unsupported conversion target")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("data", name)
	if err != nil {
		return nil, err
	}
	if _, err = part.Write(data); err != nil {
		return nil, err
	}
	if err = form.Close(); err != nil {
		return nil, err
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(call, "POST", c.ServerURL+Prefix+"/cool/convert-to/"+target, &body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", form.FormDataContentType())
	client, err := servicehttp.Client(ctx, c.CAFile)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("document conversion unavailable for this source")
	}
	result, err := io.ReadAll(io.LimitReader(response.Body, (10<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(result) > 10<<20 {
		return nil, errors.New("converted document exceeds size limit")
	}
	return result, nil
}
