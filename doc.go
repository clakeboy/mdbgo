// Package mdbgo 提供对 MDB 文件的 Go 读取 API。
//
// 当前实现采用 bundled 模式：构建时直接编译仓库自带的 C 源码，
// 不要求调用方预装系统级 libmdb。
package mdbgo
