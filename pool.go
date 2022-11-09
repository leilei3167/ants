// MIT License

// Copyright (c) 2018 Andy Pan

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package ants

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/panjf2000/ants/v2/internal"
)

// Pool accepts the tasks from client, it limits the total of goroutines to a given number by recycling goroutines.
// 一个池中最核心的资源就是worker,由pool负责对worker的调度,work存放在一个特定的数据结构中;
type Pool struct {
	// capacity of the pool, a negative value means that the capacity of pool is limitless, an infinite pool is used to
	// avoid potential issue of endless blocking caused by nested usage of a pool: submitting a task to pool
	// which submits a new task to the same pool.
	capacity int32 //容量

	// running is the number of the currently running goroutines.
	running int32 //当前正在运行的goroutine

	// lock for protecting the worker queue.
	lock sync.Locker //一个自旋锁

	// workers is a slice that store the available workers.
	workers workerArray //存放的goWorker数据结构

	// state is used to notice the pool to closed itself.
	state int32 //用于判断池处于何种状态

	// cond for waiting to get an idle worker.
	cond *sync.Cond //用于多个goroutine等待某个特定资源(task等待worker,当worker就绪时会调用Signal)的控制

	// workerCache speeds up the obtainment of a usable worker in function:retrieveWorker.
	workerCache sync.Pool //提高worker的复用

	// waiting is the number of goroutines already been blocked on pool.Submit(), protected by pool.lock
	waiting int32 //当前在等待获取worker的Submit的数量,如果是非并发的提交执行Submit,则阻塞数量最大为1

	heartbeatDone int32              //过期检查是否完毕
	stopHeartbeat context.CancelFunc //关闭过期检查的回调

	options *Options
}

// purgePeriodically clears expired workers periodically which runs in an individual goroutine, as a scavenger.
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

		if p.IsClosed() { //如果已经关闭则退出循环
			break
		}

		p.lock.Lock()
		expiredWorkers := p.workers.retrieveExpiry(p.options.ExpiryDuration)
		p.lock.Unlock()

		// Notify obsolete workers to stop.
		// This notification must be outside the p.lock, since w.task
		// may be blocking and may consume a lot of time if many workers
		// are located on non-local CPUs.
		for i := range expiredWorkers { //因为已过期的worker开启的goroutine还阻塞在taskChan上
			//因此需要发送一个nil 是的goroutine退出,退出后解除对该worker的引用
			expiredWorkers[i].task <- nil
			expiredWorkers[i] = nil
		}

		// There might be a situation where all workers have been cleaned up(no worker is running),
		// or another case where the pool capacity has been Tuned up,
		// while some invokers still get stuck in "p.cond.Wait()",
		// then it ought to wake all those invokers.
		if p.Running() == 0 || (p.Waiting() > 0 && p.Free() > 0) {
			p.cond.Broadcast() //使所有cond.Wait处解除阻塞
		}
	}
}

// NewPool generates an instance of ants pool.
func NewPool(size int, options ...Option) (*Pool, error) {
	opts := loadOptions(options...) //选项模式,将可配置的选项单独提取成一个结构体

	if size <= 0 {
		size = -1 //负数为无限制
	}

	if !opts.DisablePurge {
		if expiry := opts.ExpiryDuration; expiry < 0 {
			return nil, ErrInvalidPoolExpiry
		} else if expiry == 0 {
			opts.ExpiryDuration = DefaultCleanIntervalTime //默认1s检查一次,超过1s没有被使用的worker将被清理
		}
	}

	if opts.Logger == nil { //补齐没有设置的opts部分字段
		opts.Logger = defaultLogger
	}

	p := &Pool{
		capacity: int32(size),
		lock:     internal.NewSpinLock(),
		options:  opts, //持有配置
	}
	p.workerCache.New = func() interface{} { //sync.Pool的创建方法,节约goWorker的创建成本
		return &goWorker{
			pool: p,
			task: make(chan func(), workerChanCap),
		}
	}
	if p.options.PreAlloc { //如果设置预先分配容量,则使用循环队列管理worker,否则使用栈管理
		if size == -1 {
			return nil, ErrInvalidPreAllocSize
		}
		p.workers = newWorkerArray(loopQueueType, size)
	} else {
		p.workers = newWorkerArray(stackType, 0)
	}

	p.cond = sync.NewCond(p.lock) //TODO:cond的用法?条件变量

	// Start a goroutine to clean up expired workers periodically.
	var ctx context.Context
	ctx, p.stopHeartbeat = context.WithCancel(context.Background())
	if !p.options.DisablePurge {
		go p.purgePeriodically(ctx)
	}
	return p, nil
}

// ---------------------------------------------------------------------------

// Submit submits a task to this pool.
//
// Note that you are allowed to call Pool.Submit() from the current Pool.Submit(),
// but what calls for special attention is that you will get blocked with the latest
// Pool.Submit() call once the current Pool runs out of its capacity, and to avoid this,
// you should instantiate a Pool with ants.WithNonblocking(true).
func (p *Pool) Submit(task func()) error {
	if p.IsClosed() {
		return ErrPoolClosed
	}
	var w *goWorker                       //Submit可能也是并发被调用的
	if w = p.retrieveWorker(); w == nil { //当调用Submit提交一个任务时,尝试获取可用的worker
		return ErrPoolOverload
	}
	w.task <- task //如果获取到了可用的worker 将任务交放入他的taskChan
	return nil
}

// Running returns the number of workers currently running.
func (p *Pool) Running() int {
	return int(atomic.LoadInt32(&p.running))
}

// Free returns the number of available goroutines to work, -1 indicates this pool is unlimited.
func (p *Pool) Free() int {
	c := p.Cap()
	if c < 0 {
		return -1
	}
	return c - p.Running()
}

// Waiting returns the number of tasks which are waiting be executed.
func (p *Pool) Waiting() int {
	return int(atomic.LoadInt32(&p.waiting))
}

// Cap returns the capacity of this pool.
func (p *Pool) Cap() int {
	return int(atomic.LoadInt32(&p.capacity))
}

// Tune changes the capacity of this pool, note that it is noneffective to the infinite or pre-allocation pool.
func (p *Pool) Tune(size int) {
	capacity := p.Cap()
	if capacity == -1 || size <= 0 || size == capacity || p.options.PreAlloc {
		return
	}
	atomic.StoreInt32(&p.capacity, int32(size))
	if size > capacity {
		if size-capacity == 1 { //只大一个容量时,唤醒一个goroutine
			p.cond.Signal()
			return
		}
		p.cond.Broadcast() //唤醒所有
	}
}

// IsClosed indicates whether the pool is closed.
func (p *Pool) IsClosed() bool {
	return atomic.LoadInt32(&p.state) == CLOSED
}

// Release closes this pool and releases the worker queue.
func (p *Pool) Release() {
	if !atomic.CompareAndSwapInt32(&p.state, OPENED, CLOSED) { //如果是已CLOSED的 则不做任何操作
		return //避免重复关闭
	}
	p.lock.Lock()
	p.workers.reset()
	p.lock.Unlock()
	// There might be some callers waiting in retrieveWorker(), so we need to wake them up to prevent
	// those callers blocking infinitely.
	p.cond.Broadcast()
}

// ReleaseTimeout is like Release but with a timeout, it waits all workers to exit before timing out.
func (p *Pool) ReleaseTimeout(timeout time.Duration) error {
	if p.IsClosed() || p.stopHeartbeat == nil {
		return ErrPoolClosed
	}

	p.stopHeartbeat()
	p.stopHeartbeat = nil
	p.Release()

	endTime := time.Now().Add(timeout)
	for time.Now().Before(endTime) {
		if p.Running() == 0 && (p.options.DisablePurge || atomic.LoadInt32(&p.heartbeatDone) == 1) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return ErrTimeout
}

// Reboot reboots a closed pool.
func (p *Pool) Reboot() {
	if atomic.CompareAndSwapInt32(&p.state, CLOSED, OPENED) { //close状态才操作
		atomic.StoreInt32(&p.heartbeatDone, 0)
		var ctx context.Context
		ctx, p.stopHeartbeat = context.WithCancel(context.Background()) //重新开启一个定期清理(被关闭时已经退出)
		if !p.options.DisablePurge {
			go p.purgePeriodically(ctx)
		}
	}
}

// ---------------------------------------------------------------------------

func (p *Pool) addRunning(delta int) {
	atomic.AddInt32(&p.running, int32(delta))
}

func (p *Pool) addWaiting(delta int) {
	atomic.AddInt32(&p.waiting, int32(delta))
}

// retrieveWorker returns an available worker to run the tasks.
func (p *Pool) retrieveWorker() (w *goWorker) {
	spawnWorker := func() { //创建worker,直接run,run会使其开启一个goroutine
		w = p.workerCache.Get().(*goWorker) //从sync.Pool中获取worker
		w.run()
	}

	p.lock.Lock() //先上锁,并发安全

	w = p.workers.detach()
	if w != nil { // first try to fetch the worker from the queue
		p.lock.Unlock() //不为nil说明成功获取到了一个worker,可以直接返回
	} else if capacity := p.Cap(); capacity == -1 || capacity > p.Running() { //容量为无限或者容量没有被用完,直接创建新的worker即可
		//如果池容量没有用完(容量大于正在运行的worker数量,直接创建新的worker即可)
		// if the worker queue is empty and we don't run out of the pool capacity,
		// then just spawn a new worker goroutine.
		p.lock.Unlock()
		spawnWorker()
	} else { // otherwise, we'll have to keep them blocked and wait for at least one worker to be put back into pool.
		//容量已满,判断是否开启非阻塞模式,开启的话直接返回nil
		if p.options.Nonblocking {
			p.lock.Unlock()
			return
		}
		//开启了阻塞模式
	retry:
		//开启了阻塞等待模式,且当前等待的任务数量大于等于最大等待数量,直接返回nil
		if p.options.MaxBlockingTasks != 0 && p.Waiting() >= p.options.MaxBlockingTasks {
			p.lock.Unlock()
			return
		}
		p.addWaiting(1)
		//cond.Wait会将当前的goroutine挂起(会调用内部的p.lock.Unlock)
		//cond.Signal或者Broadcast会唤醒,被唤醒的goroutine内部会重新指向lock的操作,所以wait之后的逻辑是在
		//有锁的条件下进行的
		p.cond.Wait() // block and wait for an available worker
		//当worker执行完一个任务时,会调用insert方法将worker放回,并调用cond.Signal通知此处解除等待
		//然后等待数量减1
		p.addWaiting(-1)

		if p.IsClosed() { //解除等待后首先应判断是否关闭
			p.lock.Unlock()
			return
		}

		var nw int
		if nw = p.Running(); nw == 0 { // awakened by the scavenger
			p.lock.Unlock()
			spawnWorker() //没有worker,创建新的worker
			return
		}
		if w = p.workers.detach(); w == nil { //有worker,取出一个;此操作可能会失败,因为可能会有多个goroutine在等待
			if nw < p.Cap() { //在运行的没有达到容量,则创建
				p.lock.Unlock()
				spawnWorker()
				return
			}
			goto retry //否则再次等待
		}
		p.lock.Unlock()
	}
	return
}

// revertWorker puts a worker back into free pool, recycling the goroutines.
func (p *Pool) revertWorker(worker *goWorker) bool {
	if capacity := p.Cap(); (capacity > 0 && p.Running() > capacity) || p.IsClosed() {
		p.cond.Broadcast() //唤醒所有的等待获取worker的协程
		return false
	}
	worker.recycleTime = time.Now()
	p.lock.Lock()

	// To avoid memory leaks, add a double check in the lock scope.
	// Issue: https://github.com/panjf2000/ants/issues/113
	if p.IsClosed() { //池被关闭,放回不成功
		p.lock.Unlock()
		return false
	}

	err := p.workers.insert(worker) //任务执行完毕,将worker放回
	if err != nil {
		p.lock.Unlock()
		return false
	}

	// Notify the invoker stuck in 'retrieveWorker()' of there is an available worker in the worker queue.
	p.cond.Signal() //放回一个worker,通知任意一个协程恢复
	p.lock.Unlock()
	return true
}
