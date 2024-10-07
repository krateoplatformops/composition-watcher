package http

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
)

func Request(method string, url string, data []byte) error {
	host := os.Getenv("RESOURCE_TREE_HANDLER_SERVICE_HOST")
	port := os.Getenv("RESOURCE_TREE_HANDLER_SERVICE_PORT")
	if host == "" || port == "" {
		return fmt.Errorf("no target webservice found")
	}

	switch method {
	case "POST":
		return post(fmt.Sprintf("http://%s:%s%s", host, port, url), data)
	case "DELETE":
		return delete(fmt.Sprintf("http://%s:%s%s", host, port, url))
	default:
		return fmt.Errorf("method not allowed")
	}
}

func post(url string, data []byte) error {
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("could not create http POST request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not send http POST form: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("received error from webservice: %s", resp.Status)
	}

	return nil
}

func delete(url string) error {
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("could not create http DELETE request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("could not send http DELETE: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("received error from webservice: %s", resp.Status)
	}
	return nil
}
