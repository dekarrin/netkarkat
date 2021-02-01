package console

import "dekarrin/netkarkat/internal/verbosity"

func getSplashTextArt() []string {
	return []string{
		"-- netkkUser [NU] began pestering netKarkat [CG] at 04:13 --",
		"",
		"                  _.-,",
		"         A    _.-´  /",
		"       .´ \\--´     /_____",
		"     ,´                  |__.-´",
		"    / ,._                ,-,_/",
		"   (  \\__)               \\/ \\",
		"  ,-      _     ,      _     \\_",
		".´       \\ \\_  | v\\  _/ \\    .´",
		" `.    ´\\/   \\ /   |/    |.-´",
		"   `.  \\      V    v     |",
		"     \\__\\ <_O_>    <_o_> |",
		"       \\    -´      `-   '",
		"        \\    -vVVVVv-  .'",
		"         `._       _.-´",
		"            `----´´",
		"",
	}
}

func printSplashTextArt(xCoord int, outputter verbosity.OutputWriter) {
	tabBytes := make([]byte, xCoord)
	for i := 0; i < len(tabBytes); i++ {
		tabBytes[i] = 0x20
	}
	tabs := string(tabBytes)

	outputter.Info("\n")
	for _, line := range getSplashTextArt() {
		outputter.Info("%s%s\n", tabs, line)
	}
}
