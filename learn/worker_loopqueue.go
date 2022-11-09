package stna

import (
	"time"
)

// 循环队列管理预先分配的池
type loopQueue struct {
	items  []*goWorker //循环队列数组
	expiry []*goWorker //过期的worker
	//添加worker时,tail向后移动,当tail等于队列size时,说明应该回绕,需要设为0
	//在worker被取出时,head向后移动
	//当tail赶上head时,说明队列已满
	//由于head=tail时队列可能为满也可能为空,因此需要isFull来标记队列的状态
	head, tail int
	size       int
	isFull     bool
}

func newWorkerLoopQueue(size int) workerArray {
	return &loopQueue{
		items: make([]*goWorker, size), //预先分配容量
		size:  size,
	}
}

func (wq *loopQueue) len() int {
	if wq.size == 0 {
		return 0
	}
	if wq.head == wq.tail {
		//可能为空也可能为满
		if wq.isFull {
			return wq.size
		}
		return 0
	}

	if wq.tail > wq.head { //tail在head后面
		return wq.tail - wq.head
	}

	return wq.size - wq.head + wq.tail //tail在head之前(说明tail发生了回绕)
}

func (wq *loopQueue) isEmpty() bool {
	return wq.head == wq.tail && !wq.isFull
}

func (wq *loopQueue) insert(worker *goWorker) error {
	if wq.size == 0 {
		return errQueueIsReleased
	}

	if wq.isFull {
		return errQueueIsFull
	}

	//tail处添加
	wq.items[wq.tail] = worker
	wq.tail++

	if wq.tail == wq.size { //添加后判断tail是否需要回绕
		wq.tail = 0
	}
	if wq.tail == wq.head { //添加后需要判断是否已满
		wq.isFull = true
	}
	return nil
}

func (wq *loopQueue) detach() *goWorker {
	if wq.isEmpty() {
		return nil
	}

	w := wq.items[wq.head]
	wq.items[wq.head] = nil
	wq.head++
	if wq.head == wq.size {
		wq.head = 0
	}
	wq.isFull = false

	return w
}

// TODO:循环队列的临街下标
func (wq *loopQueue) retrieveExpiry(duration time.Duration) []*goWorker {
	expiryTime := time.Now().Add(-duration)
	index := wq.binarySearch(expiryTime)
}

func (wq *loopQueue) binarySearch(expiryTime time.Time) int {
	var mid, nlen, basel, tmid int
	nlen = len(wq.items) //总的循环队列大小

	//最早放入的worker的过期时间都没有到,不需回收
	if wq.isEmpty() || expiryTime.Before(wq.items[wq.head].recycleTime) {
		return -1
	}

}

func (wq *loopQueue) reset() {
	if wq.isEmpty() {
		return
	}
Releasing:
	//清空已有的队列中的元素
	if w := wq.detach(); w != nil {
		w.task <- nil
		goto Releasing
	}
	wq.items = wq.items[:0]
	wq.size = 0
	wq.head = 0
	wq.tail = 0
}
