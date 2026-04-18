package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	CoverLetter string
	ResumeTitle string
	DelayMin    int
	DelayMax    int
}

func Load() *Config {
	_ = godotenv.Load()
	letter := getEnv("COVER_LETTER", "Здравствуйте! Меня заинтересовала данная вакансия. Готов рассказать подробнее о своём опыте.")
	// Normalize escape sequences in case .env uses literal \n without quotes
	letter = strings.ReplaceAll(letter, `\n`, "\n")
	letter = strings.ReplaceAll(letter, `\t`, "\t")

	return &Config{
		CoverLetter: letter,
		ResumeTitle: getEnv("RESUME_TITLE", ""),
		DelayMin:    getEnvInt("DELAY_MIN", 4),
		DelayMax:    getEnvInt("DELAY_MAX", 9),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
