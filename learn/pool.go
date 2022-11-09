package stna

import (
	"context"
	"github.com/panjf2000/ants/v2/learn/spinlock"
	"sync"
	"sync/atomic"
	"time"
)

// Pool 代表一个协程池,负责对worker资源的管理
type Pool struct {
	cap     int32       //池的大小
	running int32       //当前池中在运行的goroutine数量
	lock    sync.Locker //持有锁,保护并发安全性,使用实现的自旋锁

	workers workerArray //存放worker的数据结构

	state       int32      //池的状态标记
	cond        *sync.Cond //实现多个等待worker来执行
	workerCache sync.Pool
	waiting     int32 //当前等待被执行的任务数量

	heartbeatDone int32              //标记过期检查是否结束
	stopHeartbeat context.CancelFunc //通过ctx来控制清理协程的退出

	options *Options //池的可供配置的信息
}

func NewPool(size int, options ...Option) (*Pool, error) {
	opts := loadOptions(options...) //创建配置项

	if size <= 0 {
		size = -1 //无限制
	}

	if !opts.DisablePurge { //设置清理的间隔(默认会开启自定清理)
		if expiry := opts.ExpiryDuration; expiry < 0 {
			return nil, ErrInvalidPoolExpiry
		} else if expiry == 0 {
			opts.ExpiryDuration = DefaultCleanIntervalTime
		}
	}

	if opts.Logger == nil {
		opts.Logger = defaultLogger
	}

	p := &Pool{cap: int32(size),
		lock:    spinlock.NewSpinLock(),
		options: opts}

	p.workerCache.New = func() interface{} {
		return &goWorker{
			pool: p,
			task: make(chan func(), workerChanCap), //单核处理器使用无缓冲channel
		}
	}
	if p.options.PreAlloc {
		//如果预先分配内存,使用循环队列管理worker,否则使用栈管理
		if size == -1 {
			return nil, ErrInvalidPreAllocSize
		}
		//TODO:实现两种数据结构
	} else {

	}
	p.cond = sync.NewCond(p.lock)

	var ctx context.Context
	ctx, p.stopHeartbeat = context.WithCancel(context.Background())
	if !p.options.DisablePurge {
		go p.purgePeriodically(ctx)
	}
	return p, nil
}

// 自动清理过期worker的工作线程
func (p *Pool) purgePeriodically(ctx context.Context) {
	heartbeat := time.NewTicker(p.options.ExpiryDuration)

	defer func() {
		heartbeat.Stop()
		atomic.StoreInt32(&p.heartbeatDone, 1)
	}()

	for {
		select {
		case <-heartbeat.C:
		case <-ctx.Done():
			return
		}

		if p.IsClosed() {
			break
		}

		p.lock.Lock()
		expiredWorkers := p.workers.retrieveExpiry(p.options.ExpiryDuration)
		p.lock.Unlock()

		//发送信号,通知已过期的worker退出
		for i := range expiredWorkers {
			expiredWorkers[i].task <- nil
			expiredWorkers[i] = nil
		}

		// There might be a situation where all workers have been cleaned up(no worker is running),
		// or another case where the pool capacity has been Tuned up,
		// while some invokers still get stuck in "p.cond.Wait()",
		// then it ought to wake all those invokers.
		//清理之后判断是否需要唤醒阻塞的任务,没有正在运行的,或者有在等待的任务并且有空闲的worker
		if p.Running() == 0 || (p.Waiting() > 0 && p.Free() > 0) {
			//唤醒所有阻塞的任务提交
			p.cond.Broadcast()
		}
	}
}

func (p *Pool) IsClosed() bool {
	return atomic.LoadInt32(&p.state) == CLOSED
}

func (p *Pool) Running() int {
	return int(atomic.LoadInt32(&p.running))
}

func (p *Pool) Waiting() int {
	return int(atomic.LoadInt32(&p.waiting))
}

func (p *Pool) Cap() int {
	return int(atomic.LoadInt32(&p.cap))
}

func (p *Pool) Free() int {
	c := p.Cap()
	if c < 0 {
		return -1
	}
	return c - p.Running()
}
