package ants

import (
	"errors"
	"time"
)

var (
	// errQueueIsFull will be returned when the worker queue is full.
	errQueueIsFull = errors.New("the queue is full")

	// errQueueIsReleased will be returned when trying to insert item to a released worker queue.
	errQueueIsReleased = errors.New("the queue length is zero")
)

type workerArray interface { //容纳worker的数据结构抽象,目前支持循环队列和栈,此处可拓展多种数据结构
	len() int                                          //worker 数量
	isEmpty() bool                                     //worker 数量是否为0
	insert(worker *goWorker) error                     //goroutine 任务执行结束后，将相应的 worker 放回workerArray中
	detach() *goWorker                                 //取出一个worker
	retrieveExpiry(duration time.Duration) []*goWorker //取出所有的过期的worker
	reset()
}

type arrayType int

const ( //两种数据结构,一个栈,一个循环队列
	stackType arrayType = 1 << iota
	loopQueueType
)

func newWorkerArray(aType arrayType, size int) workerArray {
	switch aType {
	case stackType:
		return newWorkerStack(size)
	case loopQueueType:
		return newWorkerLoopQueue(size)
	default:
		return newWorkerStack(size)
	}
}
