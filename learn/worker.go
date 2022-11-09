package stna

import "time"

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

}
