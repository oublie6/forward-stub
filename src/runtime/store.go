// store.go 维护当前生效配置及组件集合，并提供安全切换能力。
package runtime

import (
	"context"
	"forward-stub/src/config"
	"sync"
	"sync/atomic"

	"go.uber.org/multierr"
	"golang.org/x/sync/errgroup"
)

// Store 持有运行时对象的全集快照。

type recvPayloadLogOption struct {
	enabled bool
	max     int
}

// 设计说明：
//  1. 通过单把互斥锁保护元数据 map，避免复杂的细粒度锁导致状态不一致；
//  2. UpdateCache 在重建时整体替换 map，Store 提供“快照后异步处理”的能力；
//  3. 停机阶段使用并发关闭，缩短 receiver/sender 数量较多场景下的整体停机耗时。
type Store struct {
	mu sync.RWMutex

	version int64

	receivers map[string]*ReceiverState
	selectors map[string]config.SelectorConfig
	taskSets  map[string][]string
	senders   map[string]*SenderState
	tasks     map[string]*TaskState

	pipelines   map[string]*CompiledPipeline
	pipelineCfg map[string][]config.StageConfig
	// pipelineStageSigs 记录 pipeline 对应的 stage 签名序列，用于 task 级引用计数。
	pipelineStageSigs map[string][]string
	// stageCache 保存可复用 stage 实例及其被 task 使用计数。
	stageCache map[string]*StageCacheEntry

	// recvPayloadLogOptions 保存 receiver payload 日志配置只读快照，供 dispatch 热路径无锁读取。
	recvPayloadLogOptions atomic.Value // map[string]recvPayloadLogOption

	payloadLogDefaultMax int
}

// NewStore 负责该函数对应的核心逻辑，详见实现细节。
func NewStore() *Store {
	s := &Store{
		receivers:         make(map[string]*ReceiverState),
		selectors:         make(map[string]config.SelectorConfig),
		taskSets:          make(map[string][]string),
		senders:           make(map[string]*SenderState),
		tasks:             make(map[string]*TaskState),
		pipelines:         make(map[string]*CompiledPipeline),
		pipelineCfg:       make(map[string][]config.StageConfig),
		pipelineStageSigs: make(map[string][]string),
		stageCache:        make(map[string]*StageCacheEntry),
	}
	s.recvPayloadLogOptions.Store(map[string]recvPayloadLogOption{})
	return s
}

// setDispatchSubs 兼容测试中直接注入“receiver -> tasks”快照的旧方式。
// 新架构下它会构造一个仅带默认路由的测试 selector。
func (s *Store) setDispatchSubs(m map[string][]*TaskState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for receiverName, tasks := range m {
		rs := s.receivers[receiverName]
		if rs == nil {
			rs = &ReceiverState{Name: receiverName, SelectorName: "__test__"}
			s.receivers[receiverName] = rs
		}
		rs.Selector.Store(&CompiledSelector{
			Name:         "__test__",
			TasksByKey:   map[string][]*TaskState{},
			DefaultTasks: tasks,
		})
	}
}

func (s *Store) getDispatchTasks(receiver string) []*TaskState {
	s.mu.RLock()
	rs := s.receivers[receiver]
	s.mu.RUnlock()
	if rs == nil {
		return nil
	}
	selectorAny := rs.Selector.Load()
	if selectorAny == nil {
		return nil
	}
	return selectorAny.(*CompiledSelector).DefaultTasks
}

// StopAll 停止当前 store 中已注册的全部 runtime 组件。
//
// 关键策略：
//  1. 先在锁内复制快照，随后释放锁，避免将慢操作（网络 stop/close）放在临界区；
//  2. receiver/sender 通过 errgroup 并发停止，降低整体等待时间；
//  3. 使用 multierr 聚合错误，避免“只返回首个错误”导致信息丢失；
//  4. task 采用 StopGraceful，保证 in-flight 包处理完成后再退出。
func (s *Store) StopAll(ctx context.Context) error {
	s.mu.Lock()
	receivers := make([]*ReceiverState, 0, len(s.receivers))
	for _, r := range s.receivers {
		receivers = append(receivers, r)
	}
	tasks := make([]*TaskState, 0, len(s.tasks))
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	senders := make([]*SenderState, 0, len(s.senders))
	for _, se := range s.senders {
		senders = append(senders, se)
	}
	s.mu.Unlock()

	var errs error

	// receiver 停止并发执行：通常包含网络事件循环退出，可能存在等待。
	rg, rctx := errgroup.WithContext(ctx)
	var rmu sync.Mutex
	for _, r := range receivers {
		r := r
		rg.Go(func() error {
			if err := r.Recv.Stop(rctx); err != nil {
				rmu.Lock()
				errs = multierr.Append(errs, err)
				rmu.Unlock()
			}
			return nil
		})
	}
	_ = rg.Wait()

	// task 需要优雅等待 in-flight，保持顺序调用可减少并发 stop 对 CPU 的抖动。
	for _, t := range tasks {
		t.T.StopGraceful()
	}

	// sender close 通常涉及连接/资源回收，并发执行可缩短总耗时。
	sg, sctx := errgroup.WithContext(ctx)
	var smu sync.Mutex
	for _, se := range senders {
		se := se
		sg.Go(func() error {
			if err := se.S.Close(sctx); err != nil {
				smu.Lock()
				errs = multierr.Append(errs, err)
				smu.Unlock()
			}
			return nil
		})
	}
	_ = sg.Wait()

	return errs
}

// setRecvPayloadLogOptions 负责该函数对应的核心逻辑，详见实现细节。
func (s *Store) setRecvPayloadLogOptions(m map[string]recvPayloadLogOption) {
	s.recvPayloadLogOptions.Store(m)
}

// getRecvPayloadLogOption 负责该函数对应的核心逻辑，详见实现细节。
func (s *Store) getRecvPayloadLogOption(receiver string) (recvPayloadLogOption, bool) {
	v := s.recvPayloadLogOptions.Load()
	if v == nil {
		return recvPayloadLogOption{}, false
	}
	opt, ok := v.(map[string]recvPayloadLogOption)[receiver]
	return opt, ok
}
