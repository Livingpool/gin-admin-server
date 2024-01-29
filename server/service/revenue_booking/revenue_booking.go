package revenue_booking

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"time"
)

type RevenueBookingApiService struct{}

const (
	DateLayout string = "2006-01-02"
	maxRetries int    = 10
)

// 發送 request 請求，並將回傳的 response 以 model 接收
func (r *RevenueBookingApiService) DoRequestAndGetResponse(method, postUrl string, reqBody io.Reader, cookie string, resBody any) error {
	req, err := http.NewRequest(method, postUrl, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", cookie)
	if _, ok := resBody.(*string); ok {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)...")
	} else {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 50 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	// data, _ := io.ReadAll(resp.Body)
	// if err := json.Unmarshal(data, resBody); err != nil {
	// 	return err
	// resBody of type *string is for html

	switch resBody := resBody.(type) {
	case *string:
		// If resBody is a string
		resBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		*resBody = string(resBytes)
	default:
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, resBody); err != nil {
			return err
		}
	}

	defer resp.Body.Close()

	return nil
}

// @Author: 劉庭瑋
// @Function: DoRequestAndGetResponseWithRetry
// @Description: 發送 request 請求，將回傳的 response 以 model 接收，並且會重試
// @Note1: To handle rate limiting for booking.com. Implements exponential backoff & jitter.
// @Note2: Jitter is a random variation added to the wait time to prevent a thundering herd problem when many instances of your application all retry at the same time.
// @Note3: [Alternative] Use a rate limiter to limit the number of requests per second. (I read online booking.com has a rate limit of 20 requests per second)
// @Note4: 目前Booking一旦回傳429就沒救了，需終止程式。
func (r *RevenueBookingApiService) DoRequestAndGetResponseWithRetry(method, postUrl string, reqBody io.Reader, cookie string, resBody any) error {

	for i := 0; i < maxRetries; i++ {

		req, err := http.NewRequest(method, postUrl, reqBody)
		if err != nil {
			return err
		}
		req.Header.Set("Cookie", cookie)

		if _, ok := resBody.(*string); ok {
			req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}

		client := &http.Client{Timeout: 50 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
			// If the status code is not 429, decode the response body
			switch resBody := resBody.(type) {
			case *string:
				// If resBody is a string
				resBytes, err := io.ReadAll(resp.Body)
				if err != nil {
					return err
				}
				*resBody = string(resBytes)
			default:
				data, err := io.ReadAll(resp.Body)
				if err != nil {
					return err
				}
				if err := json.Unmarshal(data, resBody); err != nil {
					return err
				}
			}
			return nil
		}

		// If the status code is 429 or 5xx, wait and retry
		wait := time.Duration(math.Pow(2, float64(i))) * time.Second
		jitter := time.Duration(rand.Int63n(int64(wait))) * time.Millisecond
		fmt.Printf("=== retry : %d, wait: %f seconds, jitter: %d milliseconds ===\n", i, wait.Seconds(), jitter.Milliseconds())
		fmt.Printf("=== status code: %d ===\n", resp.StatusCode)
		fmt.Println(postUrl)
		time.Sleep(wait + jitter)
	}

	return fmt.Errorf("=== max retries exceeded ===")
}
