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

	state       int32      //池的状态标记 开启 或关闭
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

// Submit 尝试提交一个任务至池中,会尝试获取一个空闲的worker来执行此任务
func (p *Pool) Submit(task func()) error {
	if p.IsClosed() {
		return ErrPoolClosed
	}

	var w *goWorker
	if w = p.retrieveWorker(); w == nil {
		return ErrPoolOverload //获取nil一定是池等待的任务过多
	}
	//成功获取到worker后,将任务发送给他
	w.task <- task
	return nil
}

// 获取一个可用的worker(创建 或者从池中取)
func (p *Pool) retrieveWorker() (w *goWorker) {
	spawnWorker := func() {
		//赋值给具名返回值
		w = p.workerCache.Get().(*goWorker) //从worker池中生成,可以最大化worker的复用
		w.run()                             //开启worker的工作线程
	}

	p.lock.Lock() //上锁保证并发安全

	w = p.workers.detach() //从数据结构中获取
	if w != nil {          //成功获取到worker,返回
		p.lock.Unlock()
		return
	} else if capacity := p.Cap(); capacity == -1 || capacity > p.Running() { //容量为无限或者容量还充裕
		//直接创建w返回
		p.lock.Unlock()
		spawnWorker()
	} else { //容量已满暂时没有空闲worker,判断是否要阻塞等待
		if p.options.Nonblocking {
			p.lock.Unlock()
			return
		}

		//已开启了阻塞模式,则进行阻塞等待
	retry:
		//先判断等待的数量是否超过最大值
		if p.options.MaxBlockingTasks != 0 && p.Waiting() >= p.options.MaxBlockingTasks {
			p.lock.Unlock()
			return
		}
		p.addWaiting(1)
		//wait在被唤醒后必须检查条件
		//因为被唤醒不等同于条件满足,仅仅是因为被唤醒了而已
		//可以理解为，等待者被唤醒，只是得到了一次检查的机会而已
		p.cond.Wait() //在等待时会解锁,在被唤醒时会重新加锁,wait恢复时是未上锁的状态,不能假定一定会wait成功,要在一个循环中
		p.addWaiting(-1)

		//被唤醒后判断此时池是否已被关闭
		if p.IsClosed() {
			p.lock.Unlock()
			return
		}

		var nw int
		if nw = p.Running(); nw == 0 { //已经没有在运行的worker(全部被清除)
			p.lock.Unlock()
			spawnWorker()
			return
		}

		if w = p.workers.detach(); w == nil { //取出worker不一定会成功
			if nw < p.Cap() { //有多余的空间,直接创建
				p.lock.Unlock()
				spawnWorker()
				return
			}
			//没有成功获取到worker,重复直到成功为止
			goto retry
		}
		//获取成功,直接退出
		p.lock.Unlock()
	}
	return
}

// 将worker重新放回至池中,刷新其时间,并通知在等待的任务
func (p *Pool) revertWorker(worker *goWorker) bool {
	//容量,p.Running() > cp的情况发生于 中途调整了pool池的大小,运行的worker就会大于容量
	//此时需要将当前的worker消除而不是放回池中
	if cp := p.Cap(); (cp > 0 && p.Running() > cp) || p.IsClosed() {
		//容量不足或池已被关闭
		//通知所有的wait中的goroutine进行一次检查,如果池已关闭则退出 避免永久阻塞
		p.cond.Broadcast()
		return false
	}

	worker.recycleTime = time.Now()
	p.lock.Lock()

	if p.IsClosed() { //double-check
		p.lock.Unlock()
		return false
	}

	err := p.workers.insert(worker) //调用数据结构将worker插入
	if err != nil {
		p.lock.Unlock()
		return false
	}

	//该worker放回成功,唤醒一个等待的任务
	p.cond.Signal()
	p.lock.Unlock()

	return true
}

//---------------------------------------------------------------------------------------

// Tune 可在运行时修改池的容量
func (p *Pool) Tune(size int) {
	capacity := p.Cap()
	if capacity == -1 || size <= 0 || size == capacity || p.options.PreAlloc {
		//对无限容量的以及提前设置了内存分配的无效
		return
	}
	atomic.StoreInt32(&p.cap, int32(size))

	if size > capacity {
		//扩容后,考虑唤醒goroutine
		if size-capacity >= 1 {
			p.cond.Signal()
			return
		}
		//有额外的空间,唤醒更多的goroutine
		p.cond.Broadcast()
	}
}

// Release 直接关闭池,并释放底层的worker数据结构
func (p *Pool) Release() {
	if !atomic.CompareAndSwapInt32(&p.state, OPENED, CLOSED) {
		return
	}
	p.lock.Lock()
	p.workers.reset()
	p.lock.Unlock()

	//通知所有等待的goroutine,条件变化
	p.cond.Broadcast()
}

// ReleaseTimeout 等待所有worker执行完任务后退出
func (p *Pool) ReleaseTimeout(timeout time.Duration) error {
	if p.IsClosed() || p.stopHeartbeat == nil {
		return ErrPoolClosed
	}

	p.stopHeartbeat() //关闭清理线程
	p.stopHeartbeat = nil
	p.Release()

	endTime := time.Now().Add(timeout)
	for time.Now().Before(endTime) { //等待指定时长
		//直到Running归零且清理完毕
		if p.Running() == 0 && (p.options.DisablePurge || atomic.LoadInt32(&p.heartbeatDone) == 1) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ErrTimeout
}

// Reboot 将一个已经关闭的池重新打开
func (p *Pool) Reboot() {
	if atomic.CompareAndSwapInt32(&p.state, CLOSED, OPENED) { //将状态置为开启即可
		//重新启动清理线程
		atomic.StoreInt32(&p.heartbeatDone, 0)
		var ctx context.Context
		ctx, p.stopHeartbeat = context.WithCancel(context.Background())
		if !p.options.DisablePurge {
			go p.purgePeriodically(ctx)
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

func (p *Pool) addRunning(delta int) {
	atomic.AddInt32(&p.running, int32(delta))
}

func (p *Pool) addWaiting(delta int) {
	atomic.AddInt32(&p.waiting, int32(delta))
}
