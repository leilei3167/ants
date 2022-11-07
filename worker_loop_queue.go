package ants

import "time"

type loopQueue struct {
	items  []*goWorker
	expiry []*goWorker
	//头和尾相同时,为空;
	//当添加worker时,尾会向后移;在head取出时,head也会向后移,此时tail>head
	//当tail或head到达数组尾部,会出现回绕,此时可能head>tail
	//当tail赶上head时,就说明队列已满
	//而当head赶上tail时,说明队列已空
	//由于head=tail时,队列可能为空也可能满,所以需要增加一个isFull字段用于判断
	head   int
	tail   int
	size   int
	isFull bool
}

func newWorkerLoopQueue(size int) *loopQueue {
	return &loopQueue{
		items: make([]*goWorker, size),
		size:  size,
	}
}

func (wq *loopQueue) len() int {
	if wq.size == 0 {
		return 0
	}

	if wq.head == wq.tail {
		if wq.isFull {
			return wq.size
		}
		return 0
	}

	if wq.tail > wq.head {
		return wq.tail - wq.head
	}

	return wq.size - wq.head + wq.tail
}

func (wq *loopQueue) isEmpty() bool {
	return wq.head == wq.tail && !wq.isFull
}

// 循环队列入队
func (wq *loopQueue) insert(worker *goWorker) error {
	if wq.size == 0 {
		return errQueueIsReleased
	}

	if wq.isFull {
		return errQueueIsFull
	}
	wq.items[wq.tail] = worker //尾设置为worker
	wq.tail++                  //移动尾指针

	if wq.tail == wq.size { //判断是否应该回绕,等于size时已经数组越界了
		wq.tail = 0 //指向队首
	}
	if wq.tail == wq.head { //当移动tail后,判断是否已经追上head,追上说明已满
		wq.isFull = true
	}

	return nil
}

// 循环队列出队
func (wq *loopQueue) detach() *goWorker {
	if wq.isEmpty() {
		return nil
	}

	w := wq.items[wq.head]
	wq.items[wq.head] = nil //置为nil,解除引用
	wq.head++
	if wq.head == wq.size { //回绕
		wq.head = 0
	}
	wq.isFull = false //一定不为空

	return w
}

func (wq *loopQueue) retrieveExpiry(duration time.Duration) []*goWorker {
	expiryTime := time.Now().Add(-duration)
	index := wq.binarySearch(expiryTime)
	if index == -1 {
		return nil
	}
	wq.expiry = wq.expiry[:0]

	if wq.head <= index {
		wq.expiry = append(wq.expiry, wq.items[wq.head:index+1]...)
		for i := wq.head; i < index+1; i++ {
			wq.items[i] = nil
		}
	} else {
		wq.expiry = append(wq.expiry, wq.items[0:index+1]...)
		wq.expiry = append(wq.expiry, wq.items[wq.head:]...)
		for i := 0; i < index+1; i++ {
			wq.items[i] = nil
		}
		for i := wq.head; i < wq.size; i++ {
			wq.items[i] = nil
		}
	}
	head := (index + 1) % wq.size
	wq.head = head
	if len(wq.expiry) > 0 {
		wq.isFull = false
	}

	return wq.expiry
}

func (wq *loopQueue) binarySearch(expiryTime time.Time) int {
	var mid, nlen, basel, tmid int
	nlen = len(wq.items)

	// if no need to remove work, return -1
	if wq.isEmpty() || expiryTime.Before(wq.items[wq.head].recycleTime) {
		return -1
	}

	// example
	// size = 8, head = 7, tail = 4
	// [ 2, 3, 4, 5, nil, nil, nil,  1]  true position
	//   0  1  2  3    4   5     6   7
	//              tail          head
	//
	//   1  2  3  4  nil nil   nil   0   mapped position
	//            r                  l

	// base algorithm is a copy from worker_stack
	// map head and tail to effective left and right
	r := (wq.tail - 1 - wq.head + nlen) % nlen
	basel = wq.head
	l := 0
	for l <= r {
		mid = l + ((r - l) >> 1)
		// calculate true mid position from mapped mid position
		tmid = (mid + basel + nlen) % nlen
		if expiryTime.Before(wq.items[tmid].recycleTime) {
			r = mid - 1
		} else {
			l = mid + 1
		}
	}
	// return true position from mapped position
	return (r + basel + nlen) % nlen
}

func (wq *loopQueue) reset() {
	if wq.isEmpty() {
		return
	}

Releasing:
	if w := wq.detach(); w != nil {
		w.task <- nil
		goto Releasing
	}
	wq.items = wq.items[:0]
	wq.size = 0
	wq.head = 0
	wq.tail = 0
}
