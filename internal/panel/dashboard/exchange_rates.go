package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	exchangeRatesURL       = "https://open.er-api.com/v6/latest/CNY"
	exchangeRatesBodyLimit = 1 << 20
)

type ExchangeRates struct {
	Base         string             `json:"base"`
	UpdatedAt    time.Time          `json:"updatedAt"`
	NextUpdateAt time.Time          `json:"nextUpdateAt"`
	Source       string             `json:"source"`
	SourceURL    string             `json:"sourceUrl"`
	Rates        map[string]float64 `json:"rates"`
}

type ExchangeRateService struct {
	mu     sync.Mutex
	cached *ExchangeRates
}

func (s *ExchangeRateService) Read(ctx context.Context) (*ExchangeRates, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && time.Now().Before(s.cached.NextUpdateAt) {
		return s.cached, nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, exchangeRatesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建汇率请求失败：%w", err)
	}
	request.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("读取汇率失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("汇率服务返回 HTTP %d", response.StatusCode)
	}
	var body struct {
		Result     string             `json:"result"`
		ErrorType  string             `json:"error-type"`
		Base       string             `json:"base_code"`
		UpdatedAt  int64              `json:"time_last_update_unix"`
		NextUpdate int64              `json:"time_next_update_unix"`
		Rates      map[string]float64 `json:"rates"`
	}
	// The provider's answer is a few KiB; cap what an unexpected body can use.
	if err := json.NewDecoder(io.LimitReader(response.Body, exchangeRatesBodyLimit)).Decode(&body); err != nil {
		return nil, fmt.Errorf("解析汇率失败：%w", err)
	}
	if body.Result != "success" {
		return nil, fmt.Errorf("汇率服务拒绝请求：%s", body.ErrorType)
	}
	if body.Base != "CNY" || body.Rates["CNY"] != 1 {
		return nil, fmt.Errorf("汇率服务返回的基准币种无效")
	}
	// NextUpdate schedules a fresh read; it is not an expiry for the latest
	// successful provider response when the provider is late publishing an update.
	if body.UpdatedAt <= 0 || body.UpdatedAt > time.Now().Unix() || body.NextUpdate <= body.UpdatedAt {
		return nil, fmt.Errorf("汇率服务返回的更新时间无效（实际更新：%s，下次更新：%s）", time.Unix(body.UpdatedAt, 0).UTC().Format(time.RFC3339), time.Unix(body.NextUpdate, 0).UTC().Format(time.RFC3339))
	}
	rates := make(map[string]float64, 8)
	for _, currency := range []string{"CNY", "USD", "EUR", "HKD", "GBP", "JPY", "TWD", "SGD"} {
		rate := body.Rates[currency]
		if rate <= 0 {
			return nil, fmt.Errorf("汇率服务未返回有效的 %s 汇率", currency)
		}
		rates[currency] = rate
	}
	result := &ExchangeRates{
		Base:         body.Base,
		UpdatedAt:    time.Unix(body.UpdatedAt, 0).UTC(),
		NextUpdateAt: time.Unix(body.NextUpdate, 0).UTC(),
		Source:       "ExchangeRate-API",
		SourceURL:    "https://www.exchangerate-api.com",
		Rates:        rates,
	}
	s.cached = result
	return result, nil
}
