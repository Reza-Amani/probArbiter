package competeTrade

import (
	"fmt"
	"log"
	"prob/market"
	"time"

	"github.com/shopspring/decimal"
)

type operationT int

const (
	opControlledTrade operationT = iota
	opAutoTrade
	opAutoSell
	opNone
)

type CompeteTrade struct {
	Operation   operationT
	Pair        string
	Buy         decimal.Decimal
	Sell        decimal.Decimal
	Quantity    decimal.Decimal
	USDQuantity decimal.Decimal
	MaxBalance  decimal.Decimal
	MinBalance  decimal.Decimal
	//the following members are for calcualting top ones, not used afterwards
	MaxUSDBal  decimal.Decimal
	MinUSDBal  decimal.Decimal
	RoughPrice decimal.Decimal
	cage       safetyCage
	infoLog    *log.Logger
	warnLog    *log.Logger
	errLog     *log.Logger
}

const CageMinutes = 1
const CagePerCentLimit = 2

type safetyCage struct {
	cageEndTimeBuy  time.Time
	cagePriceBuy    decimal.Decimal
	cageEndTimeSell time.Time
	cagePriceSell   decimal.Decimal
}

func (o *safetyCage) allowBuy(propsedPrice decimal.Decimal) bool {
	if time.Now().Before(o.cageEndTimeBuy) && propsedPrice.GreaterThan(o.cagePriceBuy) {
		return false
	}
	o.cageEndTimeBuy = time.Now().Add(time.Minute * CageMinutes)
	o.cagePriceBuy = propsedPrice.Mul(decimal.NewFromFloat32(0.01*CagePerCentLimit + 1))
	return true
}

func (o *safetyCage) allowSell(propsedPrice decimal.Decimal) bool {
	if time.Now().Before(o.cageEndTimeSell) && propsedPrice.LessThan(o.cagePriceSell) {
		return false
	}
	o.cageEndTimeSell = time.Now().Add(time.Minute * CageMinutes)
	o.cagePriceSell = propsedPrice.Mul(decimal.NewFromFloat32(-0.01*CagePerCentLimit + 1))
	return true
}

func NewCompeteTrade(p string, b decimal.Decimal, s decimal.Decimal,
	q decimal.Decimal, u decimal.Decimal,
	max decimal.Decimal, min decimal.Decimal,
	maxu decimal.Decimal, minu decimal.Decimal, roughP decimal.Decimal,
	info *log.Logger, warn *log.Logger, er *log.Logger) *CompeteTrade {

	if max.IsZero() {
		if !roughP.IsZero() {
			max = maxu.Div(roughP)
		}
	}
	if max.IsZero() {
		max = q
	}
	if max.IsZero() {
		//if max is still zero, set it from the USDquantity
		if !roughP.IsZero() {
			max = u.Div(roughP)
		}
	}
	if max.IsZero() {
		fmt.Println("ERROR: max is still zero")
	}
	if min.IsZero() {
		if !roughP.IsZero() {
			min = minu.Div(roughP)
		}
	}

	t := CompeteTrade{Pair: p, Buy: b, Sell: s, Quantity: q, USDQuantity: u,
		MaxBalance: max, MinBalance: min,
		MaxUSDBal: maxu, MinUSDBal: minu, RoughPrice: roughP,
		infoLog: info, warnLog: warn, errLog: er}

	if t.Quantity.IsZero() && t.USDQuantity.IsZero() {
		t.errLog.Println("one of quantity and payusd must be non zero")
		return nil
	}
	if !t.Quantity.IsZero() && !t.USDQuantity.IsZero() {
		t.errLog.Println("one of quantity and payusd must be zero")
		return nil
	}
	return &t
}
func (o *CompeteTrade) sellPutCheck(m *market.MarketPair) bool {
	if o.Sell.IsZero() || !m.MyLowestSell.Price.IsZero() {
		o.infoLog.Println("not selling")
		return false
	}
	s := m.MarketLowestSell.Price
	s = s.Sub(m.GetIncrement())
	o.infoLog.Println(o.Pair, "sell check:", s)
	if s.LessThan(o.Sell) || s.LessThanOrEqual(m.MarketHighestBuy.Price) {
		o.infoLog.Println("no suitable sell price", s)
		return false
	}
	var qToSell decimal.Decimal
	if !o.Quantity.IsZero() {
		qToSell = o.Quantity
	} else {
		qToSell = o.USDQuantity.DivRound(s, int32(m.Spec.QuantityPrecision))
	}
	bl, av := m.GetBalanceAndAvail()
	q := decimal.Min(qToSell, av, bl.Sub(o.MinBalance))
	q = q.Truncate(int32(m.Spec.QuantityPrecision))
	minQ, _ := decimal.NewFromString(m.Spec.MinQuantity)
	minC, _ := decimal.NewFromString(m.Spec.MinCost)
	//	o.infoLog.Println("q and minQ and minCost:", q, minQ, minC)
	if q.LessThan(minQ) || q.Mul(s).LessThan(minC) {
		o.infoLog.Println("no suitable sell quantity", q)
		return false
	}
	r := market.Order{
		LimitPrice:  s.String(),
		MarketID:    o.Pair,
		Quantity:    q.String(),
		Side:        "sell",
		TimeInForce: "gtc", //good till cancel
		Type:        "limit",
		//	r.ClientOrderID = "testsell"
	}
	m.NewOrder(r)
	o.infoLog.Println("put sell at:", r.LimitPrice)
	return true
}
func (o *CompeteTrade) buyPutCheck(m *market.MarketPair) bool {
	if o.Buy.IsZero() || !m.MyHighestBuy.Price.IsZero() {
		o.infoLog.Println("not buying")
		return false
	}
	b := m.MarketHighestBuy.Price
	b = b.Add(m.GetIncrement())
	o.infoLog.Println(o.Pair, "buy check:", b)
	if b.GreaterThan(o.Buy) || b.GreaterThanOrEqual(m.MarketLowestSell.Price) {
		o.infoLog.Println("no suitable buy price", b)
		return false
	}
	var qToBuy decimal.Decimal
	if !o.Quantity.IsZero() {
		qToBuy = o.Quantity
	} else {
		qToBuy = o.USDQuantity.DivRound(b, int32(m.Spec.QuantityPrecision))
	}
	bl, _ := m.GetBalanceAndAvail()
	q := decimal.Min(qToBuy, o.MaxBalance.Sub(bl))
	q = q.Truncate(int32(m.Spec.QuantityPrecision))
	minQ, _ := decimal.NewFromString(m.Spec.MinQuantity)
	minC, _ := decimal.NewFromString(m.Spec.MinCost)
	if q.LessThan(minQ) || q.Mul(b).LessThan(minC) {
		o.infoLog.Println("no suitable buy quantity", q)
		return false
	}
	r := market.Order{
		LimitPrice:  b.String(),
		MarketID:    o.Pair,
		Quantity:    q.String(),
		Side:        "buy",
		TimeInForce: "gtc", //good till cancel
		Type:        "limit",
		//	r.ClientOrderID = "testsell"
		//	r.Cost = "3.37"
	}
	m.NewOrder(r)
	o.infoLog.Println("put buy at:", r.LimitPrice)
	return true
}

func (o *CompeteTrade) sellOutbidCheck(m *market.MarketPair) bool {
	if !o.Sell.IsZero() && m.MyLowestSell.Price.GreaterThan(m.MarketLowestSell.Price) {
		o.infoLog.Println("outbid sell canceling", m.MyLowestSell.Price, m.MarketLowestSell.Price)
		m.CancelOrders("sell") //just cancel the existing order.
		//On the next notification caused by this cancelation, put the new order
		return true
	}
	return false
}
func (o *CompeteTrade) buyOutbidCheck(m *market.MarketPair) bool {
	if !o.Buy.IsZero() && !m.MyHighestBuy.Price.IsZero() && m.MyHighestBuy.Price.LessThan(m.MarketHighestBuy.Price) {
		o.infoLog.Println("outbid buy canceling", m.MyHighestBuy.Price, m.MarketHighestBuy.Price)
		m.CancelOrders("buy") //just cancel the existing order.
		//On the next notification caused by this cancelation, put the new order
		return true
	}
	return false
}
func (o *CompeteTrade) sellGapCheck(m *market.MarketPair) bool {

	if o.Sell.IsZero() || m.MyLowestSell.Price.IsZero() {
		return false
	}
	//check if there is a gap between me and my rivals
	p := m.MyLowestSell.Price
	p = p.Add(m.GetIncrement())
	if !p.Equal(m.MarketLowestSell.Price) && !p.Equal(m.Market2ndSell.Price) {
		m.CancelOrders("sell")
		o.infoLog.Println(o.Pair, "Cancel sells to fill the gap", m.MyLowestSell.Price, "+", m.GetIncrement(), p)
		return true
	} //for now, just cancel the existing order.On the next notification caused by this cancelation, put the new order
	return false
}
func (o *CompeteTrade) buyGapCheck(m *market.MarketPair) bool {

	if o.Buy.IsZero() || m.MyHighestBuy.Price.IsZero() {
		return false
	}
	//check if there is a gap between me and my rivals
	p := m.MyHighestBuy.Price
	p = p.Sub(m.GetIncrement())
	if !p.Equal(m.MarketHighestBuy.Price) && !p.Equal(m.Market2ndBuy.Price) {
		m.CancelOrders("buy")
		o.infoLog.Println(o.Pair, "Cancel buys to fill the gap", m.MyHighestBuy.Price, "-", m.GetIncrement(), p)
		return true
	} //for now, just cancel the existing order.On the next notification caused by this cancelation, put the new order
	return false
}
func (o *CompeteTrade) CallBackHttp(m *market.MarketPair) {
	o.infoLog.Println(o.Pair, "callbackHttp: my:", m.MyLowestSell.Price, m.MyHighestBuy.Price, "market:", m.Market2ndSell, m.MarketLowestSell.Price, m.MarketHighestBuy.Price, m.Market2ndBuy)
	if o.sellPutCheck(m) {
		return
	}
	if o.sellOutbidCheck(m) {
		return
	}
	if o.sellGapCheck(m) {
		return
	}
	if o.buyPutCheck(m) {
		return
	}
	if o.buyOutbidCheck(m) {
		return
	}
	if o.buyGapCheck(m) {
		return
	}

}
