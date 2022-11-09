package stna

import (
	"errors"
	"time"
)

const (
	stackType arrayType = 1 << iota
	loopQueueType
)

var (
	// errQueueIsFull will be returned when the worker queue is full.
	errQueueIsFull = errors.New("the queue is full")

	// errQueueIsReleased will be returned when trying to insert item to a released worker queue.
	errQueueIsReleased = errors.New("the queue length is zero")
)

// 代表能够存放worker的数据结构的抽象
type workerArray interface {
	len() int                                          //worker的数量
	isEmpty() bool                                     //是否为空
	insert(worker *goWorker) error                     //插入一个worker
	detach() *goWorker                                 //取出一个worker
	retrieveExpiry(duration time.Duration) []*goWorker //检查所有worker,取出超过一定时间未被使用的worker
	reset()                                            //清空数据结构
}

type arrayType int

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
