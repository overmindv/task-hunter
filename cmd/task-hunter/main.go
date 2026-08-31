// task-hunter — сервис сбора и нормализации кандидатов задач.
package main

import (
	"os"

	"github.com/overmindv/parker"

	"github.com/overmindv/task-hunter/internal/app/taskhunter"
)

// main запускает task-hunter через каркас parker.
func main() {
	os.Exit(parker.Main(run, parker.WithAppName("task-hunter")))
}

// run регистрирует бизнес-логику на каркас parker.
func run(app *parker.App) error {
	return taskhunter.Build(app)
}
