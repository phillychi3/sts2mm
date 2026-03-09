package main

import (
	"fmt"
	"os"
	sts2mm "phillychi3/sts2mm/src"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	p := tea.NewProgram(sts2mm.NewModel(), tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
