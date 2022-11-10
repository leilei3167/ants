package stna

import (
	"runtime"
	"time"
)

// 代表池中的worker,是被池管理的资源,任务需要获取到worker后才能被执行
type goWorker struct {
	pool *Pool       //该worker属于哪个池
	task chan func() //该worker接收任务的通道
	//记录该worker完成任务并重新放回池中的时间,此时间如果在过期间隔之前(即worker经过一段时间没有被使用),
	//此worker将被清理
	recycleTime time.Time
}

// worker是如何执行任务的?
// 每个worker启动时就会创建一个goroutine,goroutine通过channel和worker绑定,
// 该goroutine才是真正负责执行任务的
func (w *goWorker) run() {
	//当worker启动时,增加计数器
	w.pool.addRunning(1)
	//每一个worker对应一个工作协程

	go func() {
		defer func() {
			w.pool.addRunning(-1)     //工作协程应该是被复用的,需要退出时,一定是worker需要被回收,或者任务出现panic的情况
			w.pool.workerCache.Put(w) //放回

			if p := recover(); p != nil { //捕获panic并处理
				if ph := w.pool.options.PanicHandler; ph != nil {
					ph(p)
				} else { //默认panic处理
					w.pool.options.Logger.Printf("worker exits from a panic: %v\n", p)
					buf := make([]byte, 4096)
					n := runtime.Stack(buf, false)
					w.pool.options.Logger.Printf("worker exits from panic: %s\n", string(buf[:n]))
				}
			}
			w.pool.cond.Signal() //唤醒等待的任务
		}()

		for f := range w.task {
			//判断是否是退出信号
			if f == nil {
				return
			}
			f()
			//执行完毕后将此worker再次放入(可能放入不成功,如池已关闭,队列已满等,此时应该退出goroutine并释放worker)
			if ok := w.pool.revertWorker(w); !ok {
				return
			}
		}
	}()

}
