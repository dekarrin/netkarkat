package console

type macro struct {
	name    string
	content string
}

type macroset map[string]macro
