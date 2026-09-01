package main

import "embed"

// 管理台静态资源：编译进插件 exe，访问 http://<admin_listen>/ui/
//
//go:embed ui/*
var adminUI embed.FS
