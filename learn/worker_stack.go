package stna

import (
	"time"
)

type workerStack struct {
	items  []*goWorker
	expiry []*goWorker //过期的worker
}

func newWorkerStack(size int) *workerStack {
	return &workerStack{
		items:  make([]*goWorker, 0),
		expiry: nil,
	}
}

func (wq *workerStack) len() int {
	return len(wq.items)
}

func (wq *workerStack) isEmpty() bool {
	return len(wq.items) == 0
}

func (wq *workerStack) insert(worker *goWorker) error {
	wq.items = append(wq.items, worker) //直接添加到尾部
	return nil
}

func (wq *workerStack) detach() *goWorker {
	l := wq.len()
	if l == 0 {
		return nil //当没有空闲的worker时,返回nil
	}
	//有空闲的worker时,取出栈顶的一个
	w := wq.items[l-1]
	//解除对该worker的引用
	wq.items[l-1] = nil
	wq.items = wq.items[:l-1]
	return w
}

func (wq *workerStack) retrieveExpiry(duration time.Duration) []*goWorker {
	n := wq.len()
	if n == 0 {
		return nil //没有worker自然也不存在过期的worker
	}

	expiryTime := time.Now().Add(-duration) //过期的时间点=当前的时间减去时间间隔
	//找出栈中 更新时间小于expiryTime的元素下标
	index := wq.binarySearchl(0, n-1, expiryTime)

	wq.expiry = wq.expiry[:0]
	if index != -1 { //等于-1说明没有一个元素过期
		wq.expiry = append(wq.expiry, wq.items[:index+1]...) //当前index
		m := copy(wq.items, wq.items[index+1:])              //将未过期的移动至前方,然后裁剪掉
		for i := m; i < n; i++ {
			wq.items[i] = nil //已经被移动到了前方,将尾部的部分置为nil
		}
		wq.items = wq.items[:m] //只保留未过期的部分
	}
	return wq.expiry
}

func (wq *workerStack) binarySearchl(l, r int, expiryTime time.Time) int {
	var mid int
	for l <= r {
		mid = (l + r) / 2
		if expiryTime.Before(wq.items[mid].recycleTime) { //和中间元素的回收时间作比较
			//其左边的一定是比他还要更早被回收的
			r = mid - 1 //右边界左移
		} else {
			l = mid + 1
		}
	}
	return r //返回的是最晚过期的一个元素,其和其左边的元素都已过期
}

func (wq *workerStack) reset() {
	for i := 0; i < wq.len(); i++ {
		wq.items[i].task <- nil //向每个worer的chan中发送一个nil,通知该worker的goroutine退出
		wq.items[i] = nil       //解除对该worker的引用,使其被GC
	}
	wq.items = wq.items[:0] //此操作将返回一个空切片,不需要重新分配内存
}
