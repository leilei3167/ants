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
	// ErrLackPoolFunc will be returned when invokers don't provide function for pool.
	ErrLackPoolFunc = errors.New("must provide function for pool")

	// ErrInvalidPoolExpiry will be returned when setting a negative number as the periodic duration to purge goroutines.
	ErrInvalidPoolExpiry = errors.New("invalid expiry for pool")

	// ErrPoolClosed will be returned when submitting task to a closed pool.
	ErrPoolClosed = errors.New("this pool has been closed")

	// ErrPoolOverload will be returned when the pool is full and no workers available.
	ErrPoolOverload = errors.New("too many goroutines blocked on submit or Nonblocking is set")

	// ErrInvalidPreAllocSize will be returned when trying to set up a negative capacity under PreAlloc mode.
	ErrInvalidPreAllocSize = errors.New("can not set up a negative capacity under PreAlloc mode")

	// ErrTimeout will be returned after the operations timed out.
	ErrTimeout = errors.New("operation timed out")

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
