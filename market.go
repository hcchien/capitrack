package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type marketSearchResult struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Market   string `json:"market"`
	Currency string `json:"currency"`
}
type marketQuote struct {
	Symbol   string  `json:"symbol"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"`
}
type marketDataProvider interface {
	Search(context.Context, string, string) ([]marketSearchResult, error)
	Quote(context.Context, string) (marketQuote, error)
}

type publicMarketProvider struct {
	client   *http.Client
	mu       sync.Mutex
	twStocks []marketSearchResult
}

func newYahooProvider() marketDataProvider {
	return &publicMarketProvider{client: &http.Client{Timeout: 15 * time.Second}}
}

func (p *publicMarketProvider) getJSON(ctx context.Context, endpoint string, target any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 CapiTrack/0.2")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("行情服務暫時無法使用（%d）", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func (p *publicMarketProvider) Search(ctx context.Context, query, market string) ([]marketSearchResult, error) {
	if market == "TW" {
		return p.searchTaiwan(ctx, query)
	}
	var payload struct {
		Data []struct{ Symbol, Name, Asset string } `json:"data"`
	}
	endpoint := "https://api.nasdaq.com/api/autocomplete/slookup/10?search=" + url.QueryEscape(query)
	if err := p.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	results := []marketSearchResult{}
	for _, item := range payload.Data {
		if item.Asset != "STOCKS" && item.Asset != "ETF" {
			continue
		}
		results = append(results, marketSearchResult{Symbol: item.Symbol, Name: strings.TrimSpace(item.Name), Market: "US", Currency: "USD"})
		if len(results) == 8 {
			break
		}
	}
	return results, nil
}

func (p *publicMarketProvider) searchTaiwan(ctx context.Context, query string) ([]marketSearchResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.twStocks == nil {
		type company struct {
			Code      string `json:"公司代號"`
			ShortName string `json:"公司簡稱"`
			Name      string `json:"公司名稱"`
		}
		for _, source := range []struct{ url, suffix string }{{"https://openapi.twse.com.tw/v1/opendata/t187ap03_L", ".TW"}, {"https://www.tpex.org.tw/openapi/v1/mopsfin_t187ap03_O", ".TWO"}} {
			var companies []company
			if err := p.getJSON(ctx, source.url, &companies); err != nil {
				return nil, err
			}
			for _, c := range companies {
				name := strings.TrimSpace(c.ShortName)
				if name == "" {
					name = strings.TrimSpace(c.Name)
				}
				p.twStocks = append(p.twStocks, marketSearchResult{Symbol: strings.TrimSpace(c.Code) + source.suffix, Name: name, Market: "TW", Currency: "TWD"})
			}
		}
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	results := []marketSearchResult{}
	for _, stock := range p.twStocks {
		if strings.Contains(strings.ToLower(stock.Symbol), needle) || strings.Contains(strings.ToLower(stock.Name), needle) {
			results = append(results, stock)
			if len(results) == 8 {
				break
			}
		}
	}
	return results, nil
}

func (p *publicMarketProvider) Quote(ctx context.Context, symbol string) (marketQuote, error) {
	if symbol == "TWD=X" {
		var payload struct {
			Result string             `json:"result"`
			Rates  map[string]float64 `json:"rates"`
		}
		if err := p.getJSON(ctx, "https://open.er-api.com/v6/latest/USD", &payload); err != nil {
			return marketQuote{}, err
		}
		rate := payload.Rates["TWD"]
		if rate <= 0 {
			return marketQuote{}, errors.New("找不到 USD/TWD 匯率")
		}
		return marketQuote{Symbol: symbol, Price: rate, Currency: "TWD"}, nil
	}
	if strings.HasSuffix(symbol, ".TW") || strings.HasSuffix(symbol, ".TWO") {
		return p.taiwanQuote(ctx, symbol)
	}
	var payload struct {
		Data *struct {
			Symbol      string `json:"symbol"`
			PrimaryData *struct {
				LastSalePrice string `json:"lastSalePrice"`
			} `json:"primaryData"`
		} `json:"data"`
	}
	endpoint := "https://api.nasdaq.com/api/quote/" + url.PathEscape(symbol) + "/info?assetclass=stocks"
	if err := p.getJSON(ctx, endpoint, &payload); err != nil {
		return marketQuote{}, err
	}
	if payload.Data == nil || payload.Data.PrimaryData == nil {
		return marketQuote{}, errors.New("找不到美股行情")
	}
	price, err := parsePrice(payload.Data.PrimaryData.LastSalePrice)
	if err != nil {
		return marketQuote{}, err
	}
	return marketQuote{Symbol: symbol, Price: price, Currency: "USD"}, nil
}

func (p *publicMarketProvider) taiwanQuote(ctx context.Context, symbol string) (marketQuote, error) {
	code := strings.TrimSuffix(strings.TrimSuffix(symbol, ".TWO"), ".TW")
	exchange := "tse"
	if strings.HasSuffix(symbol, ".TWO") {
		exchange = "otc"
	}
	var payload struct {
		Message []struct {
			Price     string `json:"z"`
			Yesterday string `json:"y"`
		} `json:"msgArray"`
	}
	endpoint := "https://mis.twse.com.tw/stock/api/getStockInfo.jsp?ex_ch=" + exchange + "_" + url.QueryEscape(code) + ".tw"
	if err := p.getJSON(ctx, endpoint, &payload); err != nil {
		return marketQuote{}, err
	}
	if len(payload.Message) == 0 {
		return marketQuote{}, errors.New("找不到台股行情")
	}
	raw := payload.Message[0].Price
	if raw == "" || raw == "-" {
		raw = payload.Message[0].Yesterday
	}
	price, err := parsePrice(raw)
	if err != nil {
		return marketQuote{}, err
	}
	return marketQuote{Symbol: symbol, Price: price, Currency: "TWD"}, nil
}

func parsePrice(raw string) (float64, error) {
	clean := strings.NewReplacer("$", "", ",", "", "NT$", "").Replace(strings.TrimSpace(raw))
	value, err := strconv.ParseFloat(clean, 64)
	if err != nil || value <= 0 {
		return 0, errors.New("最新價格格式不正確")
	}
	return value, nil
}

func (a *app) searchAssets(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	market := strings.ToUpper(r.URL.Query().Get("market"))
	if query == "" || (market != "TW" && market != "US") {
		fail(w, errors.New("請輸入關鍵字並選擇市場"), 400)
		return
	}
	results, err := a.market.Search(r.Context(), query, market)
	if err != nil {
		fail(w, err, 502)
		return
	}
	writeJSON(w, results)
}
func (a *app) refreshMarketData(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT symbol,market FROM assets WHERE EXISTS(SELECT 1 FROM transactions t WHERE t.symbol=assets.symbol)`)
	if err != nil {
		fail(w, err, 500)
		return
	}
	symbols := []string{}
	for rows.Next() {
		var symbol, market string
		if err := rows.Scan(&symbol, &market); err != nil {
			rows.Close()
			fail(w, err, 500)
			return
		}
		if market == "TW" && !strings.Contains(symbol, ".") {
			symbol += ".TW"
		}
		symbols = append(symbols, symbol)
	}
	rows.Close()
	updated := 0
	failures := []string{}
	for _, symbol := range symbols {
		quote, err := a.market.Quote(r.Context(), symbol)
		if err != nil {
			failures = append(failures, symbol)
			continue
		}
		baseSymbol := symbol
		if strings.HasSuffix(symbol, ".TW") {
			candidate := strings.TrimSuffix(symbol, ".TW")
			var exists int
			_ = a.db.QueryRow(`SELECT count(*) FROM assets WHERE symbol=?`, candidate).Scan(&exists)
			if exists > 0 {
				baseSymbol = candidate
			}
		}
		if _, err = a.db.Exec(`UPDATE assets SET current_price=?,currency=?,updated_at=CURRENT_TIMESTAMP WHERE symbol=?`, quote.Price, quote.Currency, baseSymbol); err != nil {
			failures = append(failures, symbol)
			continue
		}
		updated++
	}
	fx, fxErr := a.market.Quote(r.Context(), "TWD=X")
	if fxErr == nil {
		_, _ = a.db.Exec(`INSERT INTO settings(key,value) VALUES('usd_twd',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprintf("%.6f", fx.Price))
	}
	now := time.Now().Format(time.RFC3339)
	_, _ = a.db.Exec(`INSERT INTO settings(key,value) VALUES('last_refresh',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, now)
	_ = a.recordSnapshot()
	writeJSON(w, map[string]any{"updated": updated, "failed": failures, "usdTwd": fx.Price, "updatedAt": now})
}
func (a *app) updateBaseCurrency(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Currency string `json:"currency"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		fail(w, err, 400)
		return
	}
	body.Currency = strings.ToUpper(body.Currency)
	if body.Currency != "TWD" && body.Currency != "USD" {
		fail(w, errors.New("顯示幣別必須是 TWD 或 USD"), 400)
		return
	}
	_, err := a.db.Exec(`INSERT INTO settings(key,value) VALUES('base_currency',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, body.Currency)
	if err != nil {
		fail(w, err, 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "currency": body.Currency})
}
