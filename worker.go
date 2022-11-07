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
	"runtime"
	"time"
)

// goWorker is the actual executor who runs the tasks,
// it starts a goroutine that accepts tasks and
// performs function calls.
type goWorker struct { //每个任务都由worker对象来处理,每个worker对象会创建一个g来执行任务
	// pool who owns this worker.
	pool *Pool //属于哪个pool

	// task is a job should be done.
	task chan func() //该worker接收任务的通道

	// recycleTime will be updated when putting a worker back into queue.
	recycleTime time.Time //记录该worker什么时候被放回池中的,完成任务被放回pool时被设置
}

// run starts a goroutine to repeat the process
// that performs the function calls.
func (w *goWorker) run() {
	w.pool.addRunning(1)
	go func() { //worker启动时创建一个g来执行任务
		defer func() { //尝试捕获panic
			w.pool.addRunning(-1)     //执行结束 减少在运行的计数器
			w.pool.workerCache.Put(w) //将worker放回
			if p := recover(); p != nil {
				if ph := w.pool.options.PanicHandler; ph != nil {
					ph(p)
				} else { //默认的panic处理
					w.pool.options.Logger.Printf("worker exits from a panic: %v\n", p)
					var buf [4096]byte
					n := runtime.Stack(buf[:], false)
					w.pool.options.Logger.Printf("worker exits from panic: %s\n", string(buf[:n]))
				}
			}
			// Call Signal() here in case there are goroutines waiting for available workers.
			w.pool.cond.Signal()
		}()

		for f := range w.task { //task关闭或者取出nil时退出,因此一个worker一直都会运行
			//每个worker值启动一次goroutine,后续一直在重复使用此goroutine
			if f == nil {
				return
			}
			f()                                    //执行任务
			if ok := w.pool.revertWorker(w); !ok { //将执行完任务的g放回pool中
				return //放回操作失败return,避免goroutine泄漏
			}
		}
	}()
}
