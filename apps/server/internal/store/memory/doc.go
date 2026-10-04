// Package memory 提供 Store seam 的进程内 Adapter。
//
// 它不是“随便写个 map”的临时假实现：同一组 Store 契约以后也会用于验证 SQLite
// Adapter。这里先用较少的基础设施练习并发安全、乐观锁、原子领取和防御性拷贝。
package memory
