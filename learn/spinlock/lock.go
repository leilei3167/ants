package spinlock

import (
	"runtime"
	"sync"
	"sync/atomic"
)

type spinLock uint32

const (
	unlock     = 0
	locked     = 1
	maxBackoff = 16
)

func (s *spinLock) Lock() {
	backoff := 1
	for !atomic.CompareAndSwapUint32((*uint32)(s), unlock, locked) {
		//加锁失败
		for i := 0; i < backoff; i++ {
			runtime.Gosched() //让出指定周期的时间片
		}
		if backoff < maxBackoff {
			backoff = backoff << 1 //指数退避
		}
	}
}

func (s *spinLock) Unlock() {
	atomic.StoreUint32((*uint32)(s), unlock)
}

func NewSpinLock() sync.Locker {
	return new(spinLock)
}
