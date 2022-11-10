package main

import (
	"fmt"
)

func main() {
	a := []int{1, 2, 3, 4, 5}
	fmt.Printf("brfore:%#v addr:%p\n", a, a)
	a = a[:0]
	fmt.Printf("after:%#v addr:%p\n", a, a)
	var b []int
	a = b[:0]
	fmt.Printf("after nil:%#v addr:%p %v\n", a, a, a == nil)
	fmt.Printf("b:%v a:%v\n", b, a)

	a = []int{1, 2, 3, 4, 5}
	a1 := a[1:]
	a2 := a[2:]
	fmt.Println(a1, a2)
	copy(a[1:], a[2:])
	fmt.Println(a[:len(a)-1])

}
