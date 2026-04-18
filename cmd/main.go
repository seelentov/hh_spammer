package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"hh_buff/internal/config"
	"hh_buff/internal/db"
	"hh_buff/internal/hh"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	repo, err := db.New("hh_spammer.db")
	if err != nil {
		log.Fatalf("Ошибка БД: %v", err)
	}

	cfg := config.Load()

	switch os.Args[1] {
	case "run":
		fs := flag.NewFlagSet("run", flag.ExitOnError)
		alwaysVisible := fs.Bool("visible", false, "Всегда показывать браузер (не сворачивать)")
		fs.BoolVar(alwaysVisible, "v", false, "Всегда показывать браузер (не сворачивать)")
		skipQuestions := fs.Bool("skip", false, "Пропускать вакансии с вопросами при отклике")
		fs.BoolVar(skipQuestions, "s", false, "Пропускать вакансии с вопросами при отклике")
		questionTimeout := fs.Duration("timeout", 2*time.Minute, "Таймаут ожидания ответов на вопросы")
		fs.DurationVar(questionTimeout, "t", 2*time.Minute, "Таймаут ожидания ответов на вопросы")
		_ = fs.Parse(os.Args[2:])

		opts := hh.Options{
			AlwaysVisible:   *alwaysVisible,
			SkipQuestions:   *skipQuestions,
			QuestionTimeout: *questionTimeout,
		}
		spammer := hh.New(cfg, repo, opts)
		if err := spammer.Run(); err != nil {
			log.Fatalf("Ошибка: %v", err)
		}

	case "add-filter":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Использование: add-filter <url> [название]")
			os.Exit(1)
		}
		name := ""
		if len(os.Args) >= 4 {
			name = os.Args[3]
		}
		if err := repo.AddFilter(os.Args[2], name); err != nil {
			log.Fatalf("Ошибка добавления: %v", err)
		}
		fmt.Println("✓ Фильтр добавлен")

	case "list-filters":
		filters, err := repo.GetAllFilters()
		if err != nil {
			log.Fatalf("Ошибка: %v", err)
		}
		if len(filters) == 0 {
			fmt.Println("Фильтров нет")
			return
		}
		fmt.Printf("%-4s %-8s %-30s %s\n", "ID", "Статус", "Название", "URL")
		fmt.Println("─────────────────────────────────────────────────────────────")
		for _, f := range filters {
			status := "активен"
			if !f.Active {
				status = "откл."
			}
			name := f.Name
			if name == "" {
				name = "(без названия)"
			}
			fmt.Printf("%-4d %-8s %-30s %s\n", f.ID, status, name, f.URL)
		}

	case "delete-filter":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Использование: delete-filter <id>")
			os.Exit(1)
		}
		id, err := strconv.ParseUint(os.Args[2], 10, 64)
		if err != nil {
			log.Fatalf("Неверный ID: %v", err)
		}
		if err := repo.DeleteFilter(uint(id)); err != nil {
			log.Fatalf("Ошибка: %v", err)
		}
		fmt.Println("✓ Фильтр удалён")

	case "disable-filter":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Использование: disable-filter <id>")
			os.Exit(1)
		}
		id, err := strconv.ParseUint(os.Args[2], 10, 64)
		if err != nil {
			log.Fatalf("Неверный ID: %v", err)
		}
		if err := repo.ToggleFilter(uint(id), false); err != nil {
			log.Fatalf("Ошибка: %v", err)
		}
		fmt.Println("✓ Фильтр отключён")

	case "enable-filter":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Использование: enable-filter <id>")
			os.Exit(1)
		}
		id, err := strconv.ParseUint(os.Args[2], 10, 64)
		if err != nil {
			log.Fatalf("Неверный ID: %v", err)
		}
		if err := repo.ToggleFilter(uint(id), true); err != nil {
			log.Fatalf("Ошибка: %v", err)
		}
		fmt.Println("✓ Фильтр включён")

	case "list-applied":
		limit := 50
		if len(os.Args) >= 3 {
			if n, err := strconv.Atoi(os.Args[2]); err == nil {
				limit = n
			}
		}
		applied, err := repo.GetApplied(limit)
		if err != nil {
			log.Fatalf("Ошибка: %v", err)
		}
		if len(applied) == 0 {
			fmt.Println("Откликов нет")
			return
		}
		for _, a := range applied {
			icon := "✓"
			extra := ""
			if !a.Success {
				icon = "✗"
				extra = fmt.Sprintf(" [%s]", a.ErrorMsg)
			}
			fmt.Printf("[%s] %s  %-40s %s%s\n",
				icon,
				a.AppliedAt.Format("2006-01-02 15:04"),
				fmt.Sprintf("%s — %s", a.Company, a.Title),
				a.URL,
				extra,
			)
		}

	default:
		fmt.Fprintf(os.Stderr, "Неизвестная команда: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`hh_spammer — автоматическая рассылка откликов на hh.ru

Команды:
  run [-v] [-s] [-t 2m]          Запустить рассылку (откроет браузер с hh.ru)
    -v, --visible                  Всегда показывать браузер (по умолч. сворачивается после входа)
    -s, --skip                     Пропускать вакансии с вопросами (по умолч. показывать браузер)
    -t, --timeout DURATION         Таймаут ожидания ответов на вопросы (по умолч. 2m)
  add-filter <url> [название]    Добавить фильтр поиска вакансий
  list-filters                   Показать все фильтры
  delete-filter <id>             Удалить фильтр
  disable-filter <id>            Отключить фильтр (не удаляя)
  enable-filter <id>             Включить фильтр
  list-applied [N]               Показать историю откликов (последние N, по умолч. 50)

Конфигурация (.env):
  COVER_LETTER=текст             Текст сопроводительного письма
  RESUME_TITLE=ключевое_слово   Часть названия нужного резюме (если несколько)
  DELAY_MIN=4                    Минимальная задержка между откликами (сек)
  DELAY_MAX=9                    Максимальная задержка между откликами (сек)

Пример:
  hh_spammer add-filter "https://hh.ru/search/vacancy?text=golang&area=1" "Go разработчик"
  hh_spammer run
`)
}
