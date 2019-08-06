package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func cmdBenchmark(input io.Reader, output io.Writer, args []string) error {
	fl := flag.NewFlagSet("", flag.ExitOnError)
	fl.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), `
Read binary serialized transaction from standard input and submit it.

Make sure to collect enough signatures before submitting the transaction.
`)
		fl.PrintDefaults()
	}
	var (
		tmAddrFl = fl.String("tm", env("BNSCLI_TM_ADDR", "https://bns.NETWORK.iov.one:443"),
			"Tendermint node address. Use proper NETWORK name. You can use BNSCLI_TM_ADDR environment variable to set it.")
	)
	fl.Parse(args)

	wsMsgs := make([]*websocket.PreparedMessage, 0)
prepareTx:
	for i := 0; ; i++ {
		tx, _, err := readTx(input)
		switch err {
		case io.EOF:
			break prepareTx
		case nil:
			// All good, continue.
		default:
			return fmt.Errorf("cannot read transaction from input: %s", err)
		}

		rawTx, err := tx.Marshal()
		if err != nil {
			return fmt.Errorf("cannot marshal transaction: %s", err)
		}
		req, err := json.Marshal(jsonrpcRequest{
			ID:      i,
			Version: "2.0",
			Method:  "broadcast_tx_commit",
			Params:  [][]byte{rawTx},
		})
		if err != nil {
			return fmt.Errorf("cannot marshal a request: %s", err)
		}

		m, err := websocket.NewPreparedMessage(websocket.TextMessage, req)
		if err != nil {
			return fmt.Errorf("cannot prepare websocket message: %s", err)
		}
		wsMsgs = append(wsMsgs, m)
	}

	tmAddr := strings.Replace(*tmAddrFl, "https://", "wss://", 1) + "/websocket"
	conn, _, err := websocket.DefaultDialer.Dial(tmAddr, nil)
	if err != nil {
		return fmt.Errorf("cannot create %q websocket connection: %s", tmAddr, err)
	}
	defer conn.Close()

	startTime := time.Now()

	var sendStats bytes.Buffer
	toConsume := make(chan int)
	go func() {
		defer close(toConsume)

		start := time.Now()
		for n, msg := range wsMsgs {
			if err := conn.WritePreparedMessage(msg); err != nil {
				fmt.Fprintf(&sendStats, "FAIL: #%d: cannot write websocket message: %s\n", n, err)
			} else {
				toConsume <- n
			}
		}
		fmt.Fprintf(&sendStats, "submit work time: %s\n", time.Now().Sub(start))
	}()

	var recvStats bytes.Buffer
	for n := range toConsume {
		var resp jsonrpcResponse
		if err := conn.ReadJSON(&resp); err != nil {
			fmt.Fprintf(&recvStats, "FAIL: #%d: cannot read response: %s\n", n, err)
			continue
		}
		if resp.Error.Code != 0 {
			fmt.Fprintf(&recvStats, "FAIL: #%d: failed response received: %+v\n", n, resp.Error)
			continue
		}
		if c := resp.Result.CheckTx; c.Code != 0 {
			fmt.Fprintf(&recvStats, "FAIL: #%d: failed check: %d: %s\n", n, c.Code, c.Log)
			continue
		}
		if d := resp.Result.DeliverTx; d.Code != 0 {
			fmt.Fprintf(&recvStats, "FAIL: #%d: failed deliver: %d: %s\n", n, d.Code, d.Log)
			continue
		}
	}
	fmt.Fprintf(&recvStats, "work time: %s\n", time.Now().Sub(startTime))

	sendStats.WriteTo(output)
	recvStats.WriteTo(output)
	return nil
}

type jsonrpcRequest struct {
	Version string   `json:"jsonrpc"`
	ID      int      `json:"id"`
	Method  string   `json:"method"`
	Params  [][]byte `json:"params"`
}

type jsonrpcResponse struct {
	Version string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Error   struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    string `json:"data"`
	} `json:"error"`
	Result struct {
		Height  string `json:"height"`
		CheckTx struct {
			Code int    `json:"code"`
			Log  string `json:"log"`
			Data string `json:"data"`
		} `json:"check_tx"`
		DeliverTx struct {
			Code int    `json:"code"`
			Log  string `json:"log"`
			Data string `json:"data"`
		} `json:"deliver_tx"`
	} `json:"result"`
}
