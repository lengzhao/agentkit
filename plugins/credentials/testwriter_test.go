package credentials

import (
	capconfigfile "github.com/lengzhao/agentkit/cap/configfile"
	rtconfigfile "github.com/lengzhao/agentkit/runtime/configfile"
)

// testConfigFileWriter 是 /env add 测试用的标准原子写实现（test-only 依赖 runtime 实现）。
var testConfigFileWriter capconfigfile.Writer = func() capconfigfile.Writer {
	w, err := rtconfigfile.New(struct{}{}, struct{}{})
	if err != nil {
		panic(err)
	}
	return w
}()
