package market

import (
	"log"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/multierr"
)

type MarketPair struct {
	comms *Comms

	pair  string
	Coin  string
	Quote string

	startTime time.Time

	ordersHttp []marketOrders

	Spec             pairSpec
	data             MarketData
	MyOrders         []currentOrder //CurrentOrdersPair
	MarketHighestBuy order
	Market2ndBuy     order
	MyHighestBuy     order
	MarketLowestSell order
	Market2ndSell    order
	MyLowestSell     order
	increment        decimal.Decimal

	callbackHttp func(m *MarketPair)

	infoLog *log.Logger
	warnLog *log.Logger
	errLog  *log.Logger
}

func NewMarketPair(p string, c *Comms, s pairSpec, callbackhttp func(m *MarketPair), info *log.Logger, warn *log.Logger, er *log.Logger) MarketPair {
	m := MarketPair{}
	m.pair = p
	m.comms = c
	m.callbackHttp = callbackhttp
	split := strings.Split(m.pair, "-")
	m.Coin = split[0]
	m.Quote = split[1]
	m.Spec = s
	m.startTime = time.Now()
	m.infoLog = info
	m.warnLog = warn
	m.errLog = er
	m.increment, _ = decimal.NewFromString(s.PriceIncrement)
	return m
}

func (o MarketPair) String() string {
	if o.increment.IsZero() {
		return "   0"
	}
	str := "    "
	str += o.Market2ndSell.Price.Sub(o.MarketLowestSell.Price).Div(o.increment).String()
	str += " " + o.MarketLowestSell.Price.Sub(o.MarketHighestBuy.Price).Div(o.increment).String()
	str += " " + o.MarketHighestBuy.Price.Sub(o.Market2ndBuy.Price).Div(o.increment).String()
	return str
}

type order struct {
	Price    decimal.Decimal
	Quantity decimal.Decimal
}
type MarketData struct {
	Channel  string `json:"channel"`
	MarketID string `json:"market_id"`
	Status   string `json:"status"`
	Lag      int    `json:"lag"`
	Ticker   struct {
		Time        time.Time `json:"time"`
		Last        string    `json:"last"`
		Low         string    `json:"low"`
		High        string    `json:"high"`
		Change      string    `json:"change"`
		BaseVolume  string    `json:"base_volume"`
		QuoteVolume string    `json:"quote_volume"`
	} `json:"ticker"`
	OrderBooks []marketOrder `json:"order_books"`
	Reset      bool          `json:"reset"`
}

type pairSpec struct {
	ID                string `json:"id"`
	BaseCurrencyID    string `json:"base_currency_id"`
	QuoteCurrencyID   string `json:"quote_currency_id"`
	MinPrice          string `json:"min_price"`
	MaxPrice          string `json:"max_price"`
	PriceIncrement    string `json:"price_increment"`
	MinQuantity       string `json:"min_quantity"`
	MaxQuantity       string `json:"max_quantity"`
	QuantityPrecision int    `json:"quantity_precision"`
	MinCost           string `json:"min_cost"`
	MaxCost           string `json:"max_cost"`
	CostPrecision     int    `json:"cost_precision"`
	TakerFeeRate      string `json:"taker_fee_rate"`
	MakerFeeRate      string `json:"maker_fee_rate"`
	ShowInUI          bool   `json:"show_in_ui"`
	Closed            bool   `json:"closed"`
}

func (o *MarketPair) SetIncrement(i decimal.Decimal) {
	o.increment = i
}
func (o *MarketPair) GetIncrement() decimal.Decimal {
	return o.increment
}

func (o *MarketPair) groomOrdersHttp(or *marketOrders) {
	//remove orders with quantity==0
	i := 0
	l := len(or.Data)
	for i < l {
		if or.Data[i].Quantity == "0" {
			or.Data = append(or.Data[:i], or.Data[i+1:]...)
			l--
		} else {
			i++
		}
	}
	or.Data = or.Data[:i]

	//find HighestBuy and LowestSell
	o.MarketHighestBuy.Price = decimal.NewFromInt(0)
	o.MarketHighestBuy.Quantity = decimal.NewFromInt(0)
	o.MarketLowestSell.Price = decimal.NewFromInt(999999)
	o.MarketLowestSell.Quantity = decimal.NewFromInt(0)
	for _, r := range or.Data {
		p, _ := decimal.NewFromString(r.Price)
		q, _ := decimal.NewFromString(r.Quantity)
		b := (r.Side == "buy")
		if b {
			if p.GreaterThanOrEqual(o.MarketHighestBuy.Price) && q.IsPositive() {
				o.MarketHighestBuy.Price = p
				o.MarketHighestBuy.Quantity = q
			}
		} else {
			if p.LessThanOrEqual(o.MarketLowestSell.Price) && q.IsPositive() {
				o.MarketLowestSell.Price = p
				o.MarketLowestSell.Quantity = q
			}
		}
	}

	//find 2ndBuy and 2ndSell
	o.Market2ndBuy.Price = decimal.NewFromInt(0)
	o.Market2ndBuy.Quantity = decimal.NewFromInt(0)
	o.Market2ndSell.Price = decimal.NewFromInt(999999)
	o.Market2ndSell.Quantity = decimal.NewFromInt(0)
	for _, r := range or.Data {
		p, _ := decimal.NewFromString(r.Price)
		q, _ := decimal.NewFromString(r.Quantity)
		b := (r.Side == "buy")
		if b {
			if p.GreaterThan(o.Market2ndBuy.Price) && q.IsPositive() && !p.Equal(o.MarketHighestBuy.Price) {
				o.Market2ndBuy.Price = p
				o.Market2ndBuy.Quantity = q
			}
		} else {
			if p.LessThan(o.Market2ndSell.Price) && q.IsPositive() && !p.Equal(o.MarketLowestSell.Price) {
				o.Market2ndSell.Price = p
				o.Market2ndSell.Quantity = q
			}
		}
	}
}
func (o *MarketPair) UpdateMarketHttp() error {
	r, er := o.comms.GetMarketOrdersHttp(o.pair)
	if er != nil {
		o.errLog.Println(er)
		return er
	}
	o.groomOrdersHttp(r)
	o.UpdateMyOrders()
	if er != nil {
		o.errLog.Println(er)
		return er
	}
	o.callbackHttp(o)
	return nil
}

//just an update on HighestBuy/Sell without any action
func (o *MarketPair) UpdateMarketEdgeOrders() error {
	r, er := o.comms.GetMarketOrdersHttp(o.pair)
	if er != nil {
		o.errLog.Println(er)
		return er
	}
	o.groomOrdersHttp(r)
	return nil
}

type Order struct {
	MarketID    string `json:"market_id"`
	Type        string `json:"type"`
	Side        string `json:"side"`
	TimeInForce string `json:"time_in_force"`
	LimitPrice  string `json:"limit_price"`
	//	Cost string `json:"cost"`	//only in market order
	//only one of cost or LimitPrice Exist
	Quantity      string `json:"quantity"`
	ClientOrderID string `json:"client_order_id"`
}

type cancelingOrder struct {
	MarketID string `json:"market_id"`
	OrderID  string `json:"order_id"`
}

func (o *MarketPair) NewOrder(r Order) error {
	return o.comms.NewOrder(r)
}
func (o *MarketPair) CancelOrders(buysell string) error {
	//cancels all orders with side buysell. buysell is either buy or sell
	o.infoLog.Println("Cancelling orders:", buysell, " for:", o.pair)
	var err error
	for _, d := range o.MyOrders {
		if d.Side == buysell && d.MarketID == o.pair {
			c := cancelingOrder{MarketID: o.pair, OrderID: d.ID}
			e := o.comms.CancelOrder(c)
			err = multierr.Append(err, e)
		}
	}
	if err != nil {
		o.infoLog.Println("error in caceling orders:", err, o.pair)
	}
	return err
}
func (o *MarketPair) UpdateMyOrders() error {
	bp, bq, sp, sq := o.MyHighestBuy.Price, o.MyHighestBuy.Quantity, o.MyLowestSell.Price, o.MyLowestSell.Quantity
	o.infoLog.Println(o.pair, "Getting existing orders")
	orders, err := o.comms.GetMyOrdersPair(o.pair)
	if err != nil {
		o.errLog.Println(o.pair, "error is recieving orders:", err)
		return err
	}
	o.MyOrders = orders

	o.MyHighestBuy = order{}
	o.MyLowestSell = order{}
	for _, d := range o.MyOrders {
		lp, _ := decimal.NewFromString(d.LimitPrice)
		q, _ := decimal.NewFromString(d.Quantity)
		can, _ := decimal.NewFromString(d.CancelledQuantity)
		q = q.Sub(can)
		if d.MarketID == o.pair && d.Side == "buy" && !q.IsZero() && lp.GreaterThanOrEqual(o.MyHighestBuy.Price) {
			o.MyHighestBuy.Price = lp
			o.MyHighestBuy.Quantity = q
		}
		if d.MarketID == o.pair && d.Side == "sell" && !q.IsZero() && (lp.LessThanOrEqual(o.MyLowestSell.Price) || o.MyLowestSell.Price.IsZero()) {
			o.MyLowestSell.Price = lp
			o.MyLowestSell.Quantity = q
		}
	}
	if !(bp.Equal(o.MyHighestBuy.Price) && bq.Equal(o.MyHighestBuy.Quantity) && sp.Equal(o.MyLowestSell.Price) && sq.Equal(o.MyLowestSell.Quantity)) {
		o.infoLog.Println(o.pair, " my edge orders: ", o.MyLowestSell.Price, "(", o.MyLowestSell.Quantity, ")  ", o.MyHighestBuy.Price, "(", o.MyHighestBuy.Quantity, ")")
	}
	return nil
}
func (o MarketPair) GetBalanceAndAvail() (decimal.Decimal, decimal.Decimal) {
	//!! TODO: check if possible: comms gets balance once for all and keep it
	return o.comms.GetBalanceAndAvail(o.Coin)
}
func (o MarketPair) ReportHistory() (decimal.Decimal, decimal.Decimal, error) {
	//returns the added amount of base and added (-spent) of quote coin
	h, err := o.comms.GetTradeHistory(o.pair, o.startTime, time.Now())
	if err != nil {
		o.errLog.Println("error in fetching hostory:", err)
		return decimal.NewFromInt(0), decimal.NewFromInt(0), err
	}
	var base, quote decimal.Decimal
	for _, t := range h.Data {
		b, e1 := decimal.NewFromString(t.Quantity)
		if e1 != nil {
			o.errLog.Println("error in fetching hostory:", e1)
			return decimal.NewFromInt(0), decimal.NewFromInt(0), e1
		}
		q, e2 := decimal.NewFromString(t.Cost)
		if e2 != nil {
			o.errLog.Println("error in fetching hostory:", e2)
			return decimal.NewFromInt(0), decimal.NewFromInt(0), e2
		}
		if t.Side == "buy" {
			base = base.Add(b)
			quote = quote.Sub(q)
		} else {
			base = base.Sub(b)
			quote = quote.Add(q)
		}
	}
	return base, quote, nil //TODO: future: consider the fee
}
