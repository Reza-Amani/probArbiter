package main

import (
	"arbiter/market"
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

func main() {
	fmt.Println("main")

	all, err := os.OpenFile("./multilogs/all.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		fmt.Println("error opening all logger:", err)
	}

	base, err := os.OpenFile("./multilogs/base.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		fmt.Println("error opening base logger:", err)
	}
	infoLog := log.New(io.MultiWriter(base, all), "       ", log.Ldate|log.Ltime|log.Lshortfile)
	warnLog := log.New(io.MultiWriter(os.Stdout, base, all), "       ", log.Ldate|log.Ltime|log.Lshortfile)
	errLog := log.New(io.MultiWriter(os.Stdout, base, all), "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
	infoLog.Println("sample infolog")

	ch := make(chan string)
	go ui(ch)

	clog, cerr := os.OpenFile("./multilogs/comms.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if cerr != nil {
		errLog.Println(cerr)
	}
	clogInfo := log.New(io.MultiWriter(clog, all), "       ", log.Ldate|log.Ltime|log.Lshortfile)
	clogWarn := log.New(io.MultiWriter(os.Stdout, clog, all), "       ", log.Ldate|log.Ltime|log.Lshortfile)
	clogError := log.New(io.MultiWriter(os.Stdout, clog, all), "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)
	c := market.NewComms(clogInfo, clogWarn, clogError)
	if c == nil {
		errLog.Println("error creating comms")
		return
	}
	err = c.FetchAllMarketSpecs()
	if err != nil {
		errLog.Println("CRIT: error in fetching market specs: ", err)
	}

	/////////////////market pair
	pair := "BTC-USDT"
	plog, err := os.OpenFile("./multilogs/"+pair+".txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		errLog.Println(err)
		return
	}
	plogInfo := log.New(io.MultiWriter(plog, all), "       ", log.Ldate|log.Ltime|log.Lshortfile)
	plogWarn := log.New(io.MultiWriter(os.Stdout, plog, all), "       ", log.Ldate|log.Ltime|log.Lshortfile)
	plogError := log.New(io.MultiWriter(os.Stdout, plog, all), "ERROR: ", log.Ldate|log.Ltime|log.Lshortfile)

	sp, err := c.GetMarketSpec(pair)
	if err != nil {
		errLog.Println("CRIT: error in fetching pair spec: ", pair, err)
		return
	}
	m := market.NewMarketPair(pair, c, sp, callBack, plogInfo, plogWarn, plogError)

	time.Sleep(time.Microsecond * 10)

	c.StartAuth()
	c.OpenSocket()
	c.RegisterPair(pair, &m)
	c.Subscribe(pair)
	for {
		select {
		case t := <-ch:
			t = strings.TrimSuffix(t, "\n")
			t = strings.TrimSuffix(t, "\r")
			split := strings.Split(t, " ")
			fmt.Println(split[0])
			if split[0] == "x" {
				warnLog.Println("exit by command")
				return
			}
		default:
		}

	}
}
func callBack(m *market.MarketPair) {
	fmt.Println("callbacked", m.MarketHighestBuy)
}
func printHelp() {

	fmt.Println("--command: h for report")
	fmt.Println("command: x for exit")
	fmt.Println("command: ? for this help")
}
func ui(ch chan<- string) {
	reader := bufio.NewReader(os.Stdin)
	printHelp()
	for {
		text, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println(err)
			return
		}
		ch <- text
		time.Sleep(time.Millisecond * 100)
	}

}
