// Package schedule 统一承载服务内定时任务框架。
//
// 当前阶段职责：
// 1. 抽离零散的 ticker / goroutine，统一收口到 schedule 模块管理；
// 2. 通过 local / leader_only 两种分组模式明确任务运行边界；
// 3. 提供 manager / group / job 的基础骨架，便于 bootstrap 统一接入生命周期；
// 4. 先基于标准库提供最小可用实现，并为后续接入 gocron 与分布式选主预留扩展点。
//
// 设计目标：
// 1. local 任务由每个实例各自执行；
// 2. leader_only 任务只在当前实例具备 leader 身份时运行；
// 3. Elector 接口先保持抽象，后续可继续接 Redis / etcd / Kubernetes 等选主实现；
// 4. 业务代码不再自行散落 ticker / goroutine，而是统一从 schedule 注册入口挂载。
package schedule
