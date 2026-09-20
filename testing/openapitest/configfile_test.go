package openapitest_test

import (
	capconfigfile "github.com/lengzhao/agentkit/cap/configfile"
	rtconfigfile "github.com/lengzhao/agentkit/runtime/configfile"
)

// newTestConfigFileWriter 返回 /add 命令测试用的标准原子写实现（test-only 依赖 runtime 实现）。
func newTestConfigFileWriter() capconfigfile.Writer {
	w, err := rtconfigfile.New(struct{}{}, struct{}{})
	if err != nil {
		panic(err)
	}
	return w
}
