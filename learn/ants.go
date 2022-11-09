package stna

import (
	"errors"
	"log"
	"math"
	"os"
	"runtime"
	"time"
)

const (
	DefaultCleanIntervalTime = time.Second
	DefaultAntsPoolSize      = math.MaxInt32
)
const (
	OPENED = iota
	CLOSED
)

var (
	ErrInvalidPoolExpiry = errors.New("invalid expiry for pool")
	// ErrInvalidPreAllocSize 当设置池容量为无限时但又要提前分配内存是返回
	ErrInvalidPreAllocSize = errors.New("can not set up a negative capacity under PreAlloc mode")

	defaultLogger = Logger(log.New(os.Stderr, "", log.LstdFlags))

	workerChanCap = func() int {
		if runtime.GOMAXPROCS(0) == 1 { //单核处理器使用无缓冲channel
			return 0
		}
		return 1
	}() //直接运行此匿名函数,而不是仅仅定义

	defaultAntsPool, _ = NewPool(DefaultAntsPoolSize)
)

// Logger 能够被本包使用的Logger
type Logger interface {
	Printf(format string, v ...interface{})
}
