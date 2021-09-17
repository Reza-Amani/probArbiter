package market

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/sacOO7/gowebsocket"
	"github.com/shopspring/decimal"
)

type marketPairer interface {
	SetIncrement(i decimal.Decimal)
	UpdateMarketData(d MarketData)
}
type Comms struct {
	socket      gowebsocket.Socket
	Token       AuthToken
	marketPairs map[string]marketPairer
	Specs       httpMarketSpec
	toBeClosed  bool

	myProbID, myProbSecret string
	RateLimitTimeout       time.Time
	//orders can be updated by socket/subscribe if timing is important; no pair is specified
	infoLog *log.Logger
	warnLog *log.Logger
	errLog  *log.Logger
}

type AuthToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	ExpiryTime  time.Time
}
type currentOrder struct {
	ID                string    `json:"id"`
	UserID            string    `json:"user_id"`
	MarketID          string    `json:"market_id"`
	Type              string    `json:"type"`
	Side              string    `json:"side"`
	Quantity          string    `json:"quantity"`
	LimitPrice        string    `json:"limit_price"`
	TimeInForce       string    `json:"time_in_force"`
	FilledCost        string    `json:"filled_cost"`
	FilledQuantity    string    `json:"filled_quantity"`
	OpenQuantity      string    `json:"open_quantity"`
	CancelledQuantity string    `json:"cancelled_quantity"`
	Status            string    `json:"status"`
	Time              time.Time `json:"time"`
	ClientOrderID     string    `json:"client_order_id"`
}

type CurrentOrdersAll struct {
	Data []currentOrder `json:"data"`
}

type newOrderJson struct {
	Data currentOrder `json:"data"`
}

type httpMarketSpec struct {
	Data []pairSpec `json:"data"`
}

func NewComms(info *log.Logger, warn *log.Logger, erro *log.Logger) *Comms {
	c := Comms{infoLog: info, warnLog: warn, errLog: erro}
	c.marketPairs = make(map[string]marketPairer)

	content, err := ioutil.ReadFile("probID.txt")
	if err != nil {
		erro.Println("ID file error", err)
		return nil
	}
	c.myProbID = string(content)
	content, err = ioutil.ReadFile("probSecret.txt")
	if err != nil {
		erro.Println("secter file error", err)
		return nil
	}
	c.myProbSecret = string(content)
	return &c
}

func (o *Comms) NeedsAuth() bool {
	nilAuth := AuthToken{}
	if o.Token == nilAuth {
		return true
	}
	if time.Now().Add(time.Minute).After(o.Token.ExpiryTime) {
		return true
	}
	return false

}

func (o *Comms) AuthSocket() {
	//This is only for socket. GET doesn't need this, only sending any not-expired token is enough

	command := `{ 
			"type": "authorization",
	"token": "`
	command = command + o.Token.AccessToken
	command = command + `"
	}`
	o.infoLog.Println(command)
	o.socket.SendText(command)
}

func (o *Comms) keepAuth() {
	for {
		if o.NeedsAuth() {
			o.warnLog.Println("Needs auth")
			if time.Now().Before(o.RateLimitTimeout) {
				o.warnLog.Println("auth waiting for rate timeout")
				for time.Now().Before(o.RateLimitTimeout) {
					time.Sleep(10 * time.Millisecond)
				}
			}
			tok, err := o.GetNewToken(o.myProbID, o.myProbSecret)
			if tok == "" || err != nil {
				o.errLog.Println("Token not read", err)
			}
		}
		time.Sleep(10 * time.Second)
	}
}
func (o *Comms) StartAuth() {
	o.warnLog.Println("waiting for authorisation")
	go o.keepAuth()

	for o.NeedsAuth() {
		time.Sleep(500 * time.Millisecond)
	}
}

func (o Comms) GetMarketSpec(p string) (pairSpec, error) {
	//from whole market specs, find and return pairSpec
	for _, s := range o.Specs.Data {
		if s.ID == p {
			return s, nil
		}
	}
	return pairSpec{}, errors.New("pairSpec not found")
}
func (o *Comms) FetchAllMarketSpecs() error {
	//get whole market specs once and store it
	req, e := http.NewRequest("GET", "https://api.probit.com/api/exchange/v1/market", nil)
	if e != nil {
		o.errLog.Println("Error in get market specs:", e)
		return e
	}
	q := req.URL.Query()
	// q.Add("limit", "1000")
	req.URL.RawQuery = q.Encode()
	o.infoLog.Println(req.URL.String())

	resp, err := http.Get(req.URL.String())
	if err != nil {
		o.errLog.Println("Error in  http.Get:", err)
		return err
	}
	defer resp.Body.Close()

	b, e := ioutil.ReadAll(resp.Body)
	if e != nil {
		o.errLog.Println("io err:", e)
		return e
	}
	err = json.Unmarshal(b, &o.Specs) //!!TODO:
	if err != nil {
		o.errLog.Println("error in reading spec: maybe: size of reader buffer:", err)
		o.errLog.Println(resp.Status)
		return err
	} else {
		o.warnLog.Println("market specs stored, len of spec:", len(o.Specs.Data))
	}
	return nil
}

func (o *Comms) GetNewToken(id string, secret string) (string, error) {
	//https://docs-en.probit.com/reference#token
	//https://blog.logrocket.com/making-http-requests-in-go/
	o.infoLog.Println("Http getting a new token")
	str := []byte(id + ":" + secret)
	b64 := base64.StdEncoding.EncodeToString(str)
	basic := "Basic " + b64

	//request.body = "{\"grant_type\":\"client_credentials\"}"
	//	req.Body = "{\"grant_type\":\"client_credentials\"}"
	postBody, _ := json.Marshal(map[string]string{
		"grant_type": "client_credentials",
	})
	responseBody := bytes.NewBuffer(postBody)

	req, e := http.NewRequest("POST", "https://accounts.probit.com/token", responseBody)
	if e != nil {
		o.errLog.Println("error in new token req:", e)
		return "", e
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", basic)
	req.Header.Add("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if resp == nil || err != nil {
		o.errLog.Println("error new token send:", err)
		return "", err
	}

	defer resp.Body.Close()

	b, e := ioutil.ReadAll(resp.Body)

	if err != nil {
		o.errLog.Println("error in reading POST response:", err)
		return "", err
	}
	o.infoLog.Println(string(b))
	err = json.Unmarshal(b, &o.Token)
	if err != nil {
		o.errLog.Println("error in unmarshaling token:", err)
		o.errLog.Println(resp.Status)
		return "", err
	}
	o.Token.ExpiryTime = time.Now().Add(time.Second * time.Duration(o.Token.ExpiresIn))
	o.infoLog.Println("New Token. expiry time:", o.Token.ExpiryTime.Format("15:04:05.000"))
	return o.Token.AccessToken, nil
}

func (o *Comms) NewOrder(r Order) error {
	//https://docs-en.probit.com/reference#order-1
	//https://blog.logrocket.com/making-http-requests-in-go/

	//request.body = "{\"grant_type\":\"client_credentials\"}"
	//	req.Body = "{\"grant_type\":\"client_credentials\"}"
	// postBody, _ := json.Marshal(map[string]string{
	// 	"grant_type": "client_credentials",
	// })
	postBody, _ := json.Marshal(r)
	responseBody := bytes.NewBuffer(postBody)
	//	o.infoLog.Println(string(responseBody.Bytes()))
	req, e := http.NewRequest("POST", "https://api.probit.com/api/exchange/v1/new_order", responseBody)
	if e != nil {
		o.errLog.Println("error in preparing new order:", e)
		return e
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", "Bearer "+o.Token.AccessToken)
	req.Header.Add("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		o.errLog.Println("error in sending new order :", err)
		return err
	}
	defer resp.Body.Close()

	b, e := ioutil.ReadAll(resp.Body)
	s := string(b)
	if strings.Contains(s, `"NOT_ENOUGH_BALANCE"`) {
		o.infoLog.Println(".")
		return nil
	}
	o.infoLog.Println(string(b))
	if err != nil {
		o.errLog.Println("error in reading POST response:", err)
		return err
	}

	newOrder := newOrderJson{}
	err = json.Unmarshal(b, &newOrder)
	if err != nil {
		o.errLog.Println("error in unmarshaling newOrder:", err)
		o.errLog.Println(resp.Status)
		if strings.Contains(resp.Status, "Too Many") {
			o.RateLimitTimeout = time.Now().Add(time.Second * 120)
			o.errLog.Println("Rate Timeout:", o.RateLimitTimeout, time.Now())
		}
		return err
	}
	// if newOrder.OpenQuantity != r.Quantity {
	// 	//o.errLog.Println("wrong parameters executed for the new order")
	// 	//TODO: newOrder needs to be slice of currentorders
	// }
	//o.errLog.Println(newOrder.Status)
	return nil
}

func (o *Comms) CancelOrder(c cancelingOrder) error {
	postBody, _ := json.Marshal(c)
	responseBody := bytes.NewBuffer(postBody)
	req, e := http.NewRequest("POST", "https://api.probit.com/api/exchange/v1/cancel_order", responseBody)
	if e != nil {
		o.errLog.Print(e)
		return e
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", "Bearer "+o.Token.AccessToken)
	req.Header.Add("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		o.errLog.Println("error :", err)
		return e
	}
	defer resp.Body.Close()

	_, e = ioutil.ReadAll(resp.Body)
	if err != nil {
		o.errLog.Println("error in reading POST response:", err)
		o.errLog.Println(resp.Status)
		if strings.Contains(resp.Status, "Too Many") {
			o.RateLimitTimeout = time.Now().Add(time.Second * 120)
			o.errLog.Println("Rate Timeout:", o.RateLimitTimeout, time.Now())
		}
		return e
	}
	// err = json.Unmarshal(b, &o.token)
	// if err != nil {
	// 	o.errLog.Println("error in unmarshaling token:", err)
	// 	return
	// }
	// o.token.ExpiryTime = time.Now().Add(time.Second * time.Duration(o.token.ExpiresIn-10))
	// o.errLog.Println("New Toke. expiry time:", o.token.ExpiryTime)
	return nil
}
func (o *Comms) GetMyOrdersPair(p string) ([]currentOrder, error) {
	orders := CurrentOrdersAll{}
	req, e := http.NewRequest("GET", "https://api.probit.com/api/exchange/v1/open_order", nil)
	if e != nil {
		o.errLog.Println("Error in get orders:", e)
		return orders.Data, e
	}
	q := req.URL.Query()
	q.Add("market_id", p)
	req.URL.RawQuery = q.Encode()
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", "Bearer "+o.Token.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		o.errLog.Println("Error in  http.Get:", err)
		return orders.Data, err
	}
	defer resp.Body.Close()

	b, e := ioutil.ReadAll(resp.Body)
	if e != nil {
		o.errLog.Println("io err:", e)
		return orders.Data, e
	}
	err = json.Unmarshal(b, &orders)
	if err != nil {
		o.errLog.Println("error in reading orders:", err)
		o.errLog.Println(resp.Status)
		if strings.Contains(resp.Status, "Too Many") {
			o.RateLimitTimeout = time.Now().Add(time.Second * 650)
			o.errLog.Println("Rate Timeout:", o.RateLimitTimeout, time.Now())
		}
		return orders.Data, err
	}
	return orders.Data, nil

}

type Balance struct {
	Data []struct {
		CurrencyID string `json:"currency_id"`
		Total      string `json:"total"`
		Available  string `json:"available"`
	} `json:"data"`
}

func (o Comms) GetBalanceAndAvail(co string) (decimal.Decimal, decimal.Decimal) {

	req, e := http.NewRequest("GET", "https://api.probit.com/api/exchange/v1/balance", nil)
	if e != nil {
		o.errLog.Println("Error in get balance:", e)
		return decimal.Zero, decimal.Zero
	}
	q := req.URL.Query()
	//	q.Add("market_id", p)
	req.URL.RawQuery = q.Encode()
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", "Bearer "+o.Token.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		o.errLog.Println("Error in  http.Get balance:", err)
		return decimal.Zero, decimal.Zero
	}
	defer resp.Body.Close()

	b, e := ioutil.ReadAll(resp.Body)
	if e != nil {
		o.errLog.Println("io err:", e)
		return decimal.Zero, decimal.Zero
	}
	var bl Balance
	err = json.Unmarshal(b, &bl)
	if err != nil {
		o.errLog.Println("error in reading balance:", err)
		o.errLog.Println(resp.Status)
		if strings.Contains(resp.Status, "Too Many") {
			o.RateLimitTimeout = time.Now().Add(time.Second * 650)
			o.errLog.Println("Rate Timeout:", o.RateLimitTimeout, time.Now())
		}
		return decimal.Zero, decimal.Zero
	}
	for _, d := range bl.Data {
		if d.CurrencyID == co {
			total, err1 := decimal.NewFromString(d.Total)
			avail, err2 := decimal.NewFromString(d.Available)
			if err1 != nil || err2 != nil {
				o.errLog.Println("error in reading total/avail:", err)
				return decimal.Zero, decimal.Zero
			}
			return total, avail
		}
	}
	return decimal.Zero, decimal.Zero

}

type TradeHistory struct {
	Data []struct {
		ID            string    `json:"id"`
		OrderID       string    `json:"order_id"`
		Side          string    `json:"side"`
		FeeAmount     string    `json:"fee_amount"`
		FeeCurrencyID string    `json:"fee_currency_id"`
		Status        string    `json:"status"`
		Price         string    `json:"price"`
		Quantity      string    `json:"quantity"`
		Cost          string    `json:"cost"`
		Time          time.Time `json:"time"`
		MarketID      string    `json:"market_id"`
	} `json:"data"`
}

func (o Comms) GetTradeHistory(p string, start time.Time, end time.Time) (*TradeHistory, error) {

	req, e := http.NewRequest("GET", "https://api.probit.com/api/exchange/v1/trade_history", nil)
	if e != nil {
		o.errLog.Println("Error in get history:", e)
		return nil, e
	}
	q := req.URL.Query()
	q.Add("market_id", p)
	q.Add("limit", "1000")
	q.Add("end_time", end.UTC().Format("2006-01-02T15:04:05")+".000Z")
	q.Add("start_time", start.UTC().Format("2006-01-02T15:04:05")+".000Z")
	req.URL.RawQuery = q.Encode()
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Authorization", "Bearer "+o.Token.AccessToken)
	o.infoLog.Println(req.URL.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		o.errLog.Println("Error in  history resp:", err)
		return nil, err
	}
	defer resp.Body.Close()

	b, e := ioutil.ReadAll(resp.Body)
	if e != nil {
		o.errLog.Println("io err:", e)
		return nil, e
	}
	h := TradeHistory{}
	err = json.Unmarshal(b, &h)
	if err != nil {
		o.errLog.Println("error in reading history:", err)
		return nil, err
	}

	return &h, nil

}

type marketTrades struct {
	Data []marketTrade `json:"data"`
}
type marketTrade struct {
	ID            string    `json:"id"`
	Price         string    `json:"price"`
	Quantity      string    `json:"quantity"`
	Time          time.Time `json:"time"`
	Side          string    `json:"side"`
	TickDirection string    `json:"tick_direction"`
}

//old structure based on the documentation:
// type marketTrade struct {
// 	ID            string    `json:"id"`
// 	OrderID       string    `json:"order_id"`
// 	Side          string    `json:"side"` //side is buy if the order had been there as a buy when a seller made the trade
// 	FeeAmount     string    `json:"fee_amount"`
// 	FeeCurrencyID string    `json:"fee_currency_id"`
// 	Status        string    `json:"status"`
// 	Price         string    `json:"price"`
// 	Quantity      string    `json:"quantity"`
// 	Cost          string    `json:"cost"`
// 	Time          time.Time `json:"time"`
// 	MarketID      string    `json:"market_id"`
// }

func (o *Comms) GetMarketTrades(p string, start time.Time, end time.Time) (*marketTrades, error) {
	req, e := http.NewRequest("GET", "https://api.probit.com/api/exchange/v1/trade", nil)
	if e != nil {
		o.errLog.Println("Error in get market trades:", e)
		return nil, e
	}
	q := req.URL.Query()
	q.Add("market_id", p)
	q.Add("limit", "1000")
	q.Add("end_time", end.UTC().Format("2006-01-02T15:04:05")+".000Z")
	q.Add("start_time", start.UTC().Format("2006-01-02T15:04:05")+".000Z")
	req.URL.RawQuery = q.Encode()
	// req.Header.Add("Accept", "application/json")
	// req.Header.Add("Authorization", "Bearer "+o.Token.AccessToken)
	o.infoLog.Println(req.URL.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		o.errLog.Println("Error in market trade resp:", err)
		return nil, err
	}
	defer resp.Body.Close()

	b, e := ioutil.ReadAll(resp.Body)
	if e != nil {
		o.errLog.Println("io err:", e)
		return nil, e
	}
	h := marketTrades{}
	err = json.Unmarshal(b, &h)
	if err != nil {
		o.errLog.Println("error in reading market trades:", err)
		return nil, err
	}

	return &h, nil

}

type marketOrders struct {
	Data []marketOrder `json:"data"`
}
type marketOrder struct {
	Side     string `json:"side"`
	Price    string `json:"price"`
	Quantity string `json:"quantity"`
}

func (o *Comms) GetMarketOrdersHttp(p string) (*marketOrders, error) {

	req, e := http.NewRequest("GET", "https://api.probit.com/api/exchange/v1/order_book", nil)
	if e != nil {
		o.errLog.Println("Error in get market orders:", e)
		return nil, e
	}
	q := req.URL.Query()
	q.Add("market_id", p)
	req.URL.RawQuery = q.Encode()
	// req.Header.Add("Accept", "application/json")
	// req.Header.Add("Authorization", "Bearer "+o.Token.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		o.errLog.Println("Error in market orders resp:", err)
		return nil, err
	}
	defer resp.Body.Close()

	b, e := ioutil.ReadAll(resp.Body)
	if e != nil {
		o.errLog.Println("io err:", e)
		return nil, e
	}
	h := marketOrders{}
	err = json.Unmarshal(b, &h)
	if err != nil {
		//o.errLog.Println("rate limit hit. test counters:", o.testEnt, o.testExit)
		//o.errLog.Println("error in reading market orders:", err)
		o.errLog.Println(resp.Status)
		if strings.Contains(resp.Status, "Too Many") {
			o.RateLimitTimeout = time.Now().Add(time.Second * 650)
			o.errLog.Println("Rate Timeout:", o.RateLimitTimeout, time.Now())
		}
		return nil, err
	}
	//resp.Header "Retry-After" [0] -> number of seconds before gate reopens
	//605(5.34.04pm)562(5.34.48pm)
	//resp.Satus "429 Too Many Requests"
	return &h, nil

}

/////////////////////////////////////Socket functions
func (o *Comms) UnregisterPair(p string) {
	o.UnSubscribe(p)
	if _, found := o.marketPairs[p]; found {
		delete(o.marketPairs, p)
	}
}
func (o *Comms) Connect() {
	o.socket = gowebsocket.New("wss://api.probit.com/api/exchange/v1/ws")

	o.socket.OnConnected = func(socket gowebsocket.Socket) {
		o.warnLog.Println("Connected to server")
	}

	o.socket.OnConnectError = func(err error, socket gowebsocket.Socket) {
		o.errLog.Println("Recieved connect error ", err)
	}

	o.socket.OnTextMessage = o.handleRXMessage

	o.socket.OnBinaryMessage = func(data []byte, socket gowebsocket.Socket) {
		o.infoLog.Println("Recieved binary data ", data)
	}

	o.socket.OnPingReceived = func(data string, socket gowebsocket.Socket) {
	}

	o.socket.OnPongReceived = func(data string, socket gowebsocket.Socket) {
		o.infoLog.Println("Recieved pong " + data)
	}

	o.socket.OnDisconnected = func(err error, socket gowebsocket.Socket) {
		o.errLog.Println("Disconnected from server ")
		if o.toBeClosed {
			o.warnLog.Println("intentional closing")
			return
		}
		o.socket.Connect()
		o.warnLog.Println("ReSubscribing pairs:", len(o.marketPairs))
		for p, _ := range o.marketPairs {
			time.Sleep(time.Millisecond * 10)
			o.Subscribe(p)
		}
		return
	}
	o.socket.Connect()
	o.warnLog.Println("Subscribing pairs:", len(o.marketPairs))
	for p, _ := range o.marketPairs {
		time.Sleep(time.Millisecond * 10)
		o.Subscribe(p)
	}
}
func (o *Comms) RegisterPair(p string, m marketPairer) error {
	s, err := o.GetMarketSpec(p)
	if err != nil {
		o.errLog.Println("ERROR: pair registration failed:", err)
		return err
	}
	i, e := decimal.NewFromString(s.PriceIncrement)
	if e != nil {
		o.errLog.Println("ERROR: parsing increment:", e)
		return err
	}
	o.marketPairs[p] = m
	m.SetIncrement(i)
	//	o.Subscribe(p)
	return nil
}
func (o *Comms) Subscribe(pair string) {

	command := `{ 
			"type": "subscribe",
	"channel": "marketdata",
	"interval": 100,
	"market_id": "`
	command = command + pair
	command = command + `",
	"filter": ["order_books"]
	}`
	o.socket.SendText(command)
}
func (o *Comms) UnSubscribe(pair string) {

	command := `{ 
			"type": "subscribe",
	"channel": "marketdata",
	"interval": 100,
	"market_id": "`
	command = command + pair
	command = command + `",
	"filter": ["ticker"]
	}`
	o.socket.SendText(command)
}
func (o *Comms) receiveMarketData(message string) {
	//o.marketpair.callback(MarketData)

	var d MarketData
	d.Reset = false
	o.infoLog.Println("Recieved MarketData: " + message)
	err := json.Unmarshal([]byte(message), &d)
	if err != nil {
		o.errLog.Println("error unmarshaling market data: ", err)
		return
	}
	m, found := o.marketPairs[d.MarketID]
	if !found {
		o.errLog.Println("market data received for a non-interested pair")
		return
	}
	m.UpdateMarketData(d)
}

func (o *Comms) handleRXMessage(message string, socket gowebsocket.Socket) {
	//	if message != "{\"type\":\"authorization\",\"result\":\"ok\"}" {
	if strings.Contains(message, `"channel":"marketdata"`) {
		//o.updateMarketData(message, socket)
		o.receiveMarketData(message)
		return
	}
	if strings.Contains(message, `"type":"authorization","result":"ok"`) {
		o.infoLog.Println("authorised: ", message)
		return
	}
	if strings.Contains(message, `"errorCode":"UNAUTHORIZED"`) {
		o.errLog.Println("UNauthorised: ", message)
		return
	}
	//{"type":"error","message":"ping timeout"}
	if strings.Contains(message, `"ping timeout"`) {
		o.errLog.Println("unhandled ERROR ping timeout: ", message)
		return
	}
	o.errLog.Println("unhandled packet")
	l := len(message)
	if l > 200 {
		l = 200
	}
	o.errLog.Println(message[:l])
}
func (o *Comms) GetMarketOrders(p string) (*marketOrders, error) {
	req, e := http.NewRequest("GET", "https://api.probit.com/api/exchange/v1/order_book", nil)
	if e != nil {
		o.errLog.Println("Error in get market orders:", e)
		return nil, e
	}
	q := req.URL.Query()
	q.Add("market_id", p)
	req.URL.RawQuery = q.Encode()
	// req.Header.Add("Accept", "application/json")
	// req.Header.Add("Authorization", "Bearer "+o.Token.AccessToken)
	o.infoLog.Println(req.URL.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		o.errLog.Println("Error in market orders resp:", err)
		return nil, err
	}
	defer resp.Body.Close()

	b, e := ioutil.ReadAll(resp.Body)
	o.infoLog.Println(string(b))
	if e != nil {
		o.errLog.Println("io err:", e)
		return nil, e
	}
	h := marketOrders{}
	err = json.Unmarshal(b, &h)
	if err != nil {
		o.errLog.Println("error in reading market orders:", err)
		o.errLog.Println(resp.Status)
		if strings.Contains(resp.Status, "Too Many") {
			o.RateLimitTimeout = time.Now().Add(time.Second * 120)
			o.errLog.Println("Rate Timeout:", o.RateLimitTimeout, time.Now())
		}
		return nil, err
	}

	return &h, nil

}
func (o *Comms) CloseSocket() {
	o.toBeClosed = true
	o.socket.Close()
}
func (o *Comms) OpenSocket() {
	o.toBeClosed = false
	o.Connect()
	o.AuthSocket()

}
