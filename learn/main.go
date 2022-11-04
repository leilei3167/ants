package main

import (
	"fmt"
	"github.com/panjf2000/ants/v2"
	"sync"
	"sync/atomic"
	"time"
)

// 参考
// https://darjun.github.io/2021/06/04/godailylib/ants-src/
// https://strikefreedom.top/archives/high-performance-implementation-of-goroutine-pool
func main() {
	defer ants.Release()

	runTimes := 100

	// 1.Use the common pool.可以任意添加需要执行的任务(满足函数签名即可),调用的是默认的defaultPool
	var wg sync.WaitGroup
	syncCalculateSum := func() {
		demoFunc()
		wg.Done() //闭包
	}
	for i := 0; i < runTimes; i++ {
		wg.Add(1)
		_ = ants.Submit(syncCalculateSum) //提交任务
	}

	wg.Wait()
	fmt.Printf("running goroutines: %d\n", ants.Running())
	fmt.Printf("finish all tasks.\n")

	//2.或者创建带有执行任务的池
	p, _ := ants.NewPoolWithFunc(10, func(i interface{}) {
		myFunc(i) //任务的参数可以靠闭包来传递
		wg.Done()
	})
	defer p.Release()
	for i := 0; i < runTimes; i++ {
		wg.Add(1)
		_ = p.Invoke(int32(i)) //args 代表传给指定的函数的参数
	}
	wg.Wait()
	fmt.Printf("running goroutines: %d\n", p.Running())
	fmt.Printf("finish all tasks, result is %d\n", sum)

	//3.创建自定义pool
	p1, _ := ants.NewPool(100, ants.WithNonblocking(true),
		ants.WithPreAlloc(true)) //非阻塞模式,并且预先分配内存(大容量池,并且耗时任务时非常有用)
	_ = p1.Submit(func() {
		fmt.Println("hello")
	})
	defer p1.Release()

	//4.在运行中可以动态的调整池的大小,并发安全
	//p1.Tune(1000) // Tune its capacity to 1000
	//p1.Tune(100000) // Tune its capacity to 100000

	//提交到pool的任务不会安装添加的顺序执行,不保证有序运行

}

var sum int32

func myFunc(i interface{}) {
	n := i.(int32)
	fmt.Printf("run with %d\n", n)
	time.Sleep(time.Millisecond * 500)
	atomic.AddInt32(&sum, n)
}

func demoFunc() {
	time.Sleep(10 * time.Millisecond)
	fmt.Println("Hello World!")
}
