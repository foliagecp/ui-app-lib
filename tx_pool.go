package uilib

import (
	"strconv"
	"time"
)

const POOL_SIZE = 5

type TxPool struct {
	tx []string
}

func (p *TxPool) GetTxID() string {
	mod := int(time.Now().Unix()) % len(p.tx)
	return p.tx[mod]
}

var pool = &TxPool{}

func init() {
	pool.tx = make([]string, 0, POOL_SIZE)
	for i := 0; i < POOL_SIZE; i++ {
		pool.tx = append(pool.tx, generateTxID(strconv.Itoa(i+int(time.Now().Unix()))))
	}
}
