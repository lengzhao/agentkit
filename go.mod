module github.com/lengzhao/agentkit

go 1.26.1

require (
	github.com/lengzhao/pluginkit v0.0.0-20260822131040-b87bdd1914a8
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/sashabaranov/go-openai v1.42.0

replace github.com/lengzhao/pluginkit => ../pluginkit
