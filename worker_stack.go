package ants

import "time"

type workerStack struct {
	items  []*goWorker //空闲的worker
	expiry []*goWorker //过期的worker
}

func newWorkerStack(size int) *workerStack {
	return &workerStack{
		items: make([]*goWorker, 0, size),
	}
}

func (wq *workerStack) len() int {
	return len(wq.items)
}

func (wq *workerStack) isEmpty() bool {
	return len(wq.items) == 0
}

func (wq *workerStack) insert(worker *goWorker) error {
	wq.items = append(wq.items, worker) //放入空闲的池中
	return nil
}

func (wq *workerStack) detach() *goWorker {
	l := wq.len()
	if l == 0 {
		return nil
	}
	//由于切片的底层结构是数组，只要有引用数组的指针，数组中的元素就不会释放。
	//这里取出切片最后一个元素后，将对应数组元素的指针设置为nil，主动释放这个引用
	w := wq.items[l-1]  //从栈中取出一个worker(先进后出,后进先出)
	wq.items[l-1] = nil // avoid memory leaks,并不会将最后一个元素置为nil,此处的元素都是内存地址
	wq.items = wq.items[:l-1]

	return w
}

func (wq *workerStack) retrieveExpiry(duration time.Duration) []*goWorker {
	n := wq.len()
	if n == 0 {
		return nil
	}

	expiryTime := time.Now().Add(-duration)      //现在的时间 减去过期的间隔
	index := wq.binarySearch(0, n-1, expiryTime) //找到最近的已过期的worker
	//由于过期时间是按照 goroutine 执行任务后的空闲时间计算的，
	//而workerStack.insert()入队顺序决定了，它们的过期时间是从早到晚的。所以可以使用二分查找

	wq.expiry = wq.expiry[:0]
	if index != -1 { //从开始到索引处worker都已经过期了
		wq.expiry = append(wq.expiry, wq.items[:index+1]...)
		m := copy(wq.items, wq.items[index+1:]) //未过期的worker复制到头部(返回复制的个数)
		for i := m; i < n; i++ {
			wq.items[i] = nil //将从m开始的所有元素置为nil(因为他们都已经被复制到了头部,取消引用防止泄漏)
		}
		wq.items = wq.items[:m]
	}
	return wq.expiry
}

// 二分查找的是最近过期的 worker，即将过期的 worker 的前一个。它和在它之前的 worker 已经全部过期了。
func (wq *workerStack) binarySearch(l, r int, expiryTime time.Time) int {
	var mid int
	for l <= r {
		mid = (l + r) / 2
		if expiryTime.Before(wq.items[mid].recycleTime) {
			r = mid - 1
		} else {
			l = mid + 1
		}
	}
	return r
}

func (wq *workerStack) reset() {
	for i := 0; i < wq.len(); i++ {
		wq.items[i].task <- nil
		wq.items[i] = nil
	}
	wq.items = wq.items[:0]
}
