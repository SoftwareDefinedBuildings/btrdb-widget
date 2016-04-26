package main

import (
	"fmt"
	"os"

	"gopkg.in/qml.v1"
)

func main() {
	if err := qml.Run(run); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	qml.RegisterTypes("BTrDBTools", 1, 0, []qml.TypeSpec{{
		Init: InitBTrDBPlotter,
	}})

	engine := qml.NewEngine()
	component, err := engine.LoadFile("demo.qml")
	if err != nil {
		return err
	}

	win := component.CreateWindow(nil)
	win.Show()
	win.Wait()
	return nil
}
