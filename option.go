package watchfile

type Option int

const (
	CheckModTime Option = 1 << iota
	CheckMD5
	CheckETag
	CheckHead
)

func (option Option) CheckModTime() bool {
	return option&CheckModTime > 0
}

func (option Option) CheckMD5() bool {
	return option&CheckMD5 > 0
}

func (option Option) CheckETag() bool {
	return option&CheckETag > 0
}

func (option Option) CheckHead() bool {
	return option&CheckHead > 0
}
