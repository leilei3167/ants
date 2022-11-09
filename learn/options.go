package stna

import (
	"time"
)

// Options 代表一个池可供配置的一些选项
type Options struct {
	ExpiryDuration   time.Duration     //经过此间隔没有被使用的worker将被清除
	PreAlloc         bool              //是否提前分配池的内存(将使用循环队列)
	MaxBlockingTasks int               //最大等待的任务数量
	Nonblocking      bool              //是否为非阻塞模式,非阻塞模式即没有可用worker时不阻塞等待,直接返回一个错误
	PanicHandler     func(interface{}) //panic的处理
	Logger           Logger
	DisablePurge     bool //是否开启自动清理空闲的worker
}

// Option 实现选项模式
type Option func(opts *Options)

func loadOptions(options ...Option) *Options {
	opts := new(Options)
	for _, o := range options {
		o(opts)
	}
	return opts
}

// WithOptions 所有设置自定义配置
func WithOptions(options Options) Option {
	return func(opts *Options) {
		*opts = options
	}
}

func WithExpiryDuration(expiryDuration time.Duration) Option {
	return func(opts *Options) {
		opts.ExpiryDuration = expiryDuration
	}
}

func WithMaxBlockingTasks(maxBlockingTasks int) Option { //设置最大等待的任务数量
	return func(opts *Options) {
		opts.MaxBlockingTasks = maxBlockingTasks
	}
}

func WithNonblocking(nonblocking bool) Option {
	return func(opts *Options) {
		opts.Nonblocking = nonblocking
	}
}

func WithPanicHandler(panicHandler func(interface{})) Option {
	return func(opts *Options) {
		opts.PanicHandler = panicHandler
	}
}

func WithLogger(logger Logger) Option {
	return func(opts *Options) {
		opts.Logger = logger
	}
}

func WithDisablePurge(disable bool) Option {
	return func(opts *Options) {
		opts.DisablePurge = disable
	}
}
