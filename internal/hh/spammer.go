package hh

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"hh_buff/internal/config"
	"hh_buff/internal/db"
)

var (
	ErrAlreadyApplied  = errors.New("already applied")
	ErrExternalVacancy = errors.New("external vacancy")
	ErrLoginTimeout    = errors.New("login timeout: more than 5 minutes passed")
	ErrQuestionsPage   = errors.New("questions page")
)

type Options struct {
	AlwaysVisible   bool
	SkipQuestions   bool
	QuestionTimeout time.Duration
}

type Spammer struct {
	browser *rod.Browser
	repo    *db.Repository
	cfg     *config.Config
	opts    Options
}

func New(cfg *config.Config, repo *db.Repository, opts Options) *Spammer {
	if opts.QuestionTimeout == 0 {
		opts.QuestionTimeout = 2 * time.Minute
	}
	return &Spammer{cfg: cfg, repo: repo, opts: opts}
}

func (s *Spammer) Run() error {
	u := launcher.New().Headless(false).MustLaunch()
	s.browser = rod.New().ControlURL(u).MustConnect()
	defer s.browser.MustClose()

	page := s.browser.MustPage("https://hh.ru/account/login")

	fmt.Println("─────────────────────────────────────────────")
	fmt.Println("  Войдите в аккаунт hh.ru в открытом браузере")
	fmt.Println("  Ожидание до 5 минут...")
	fmt.Println("─────────────────────────────────────────────")

	if err := s.waitForLogin(page); err != nil {
		return err
	}
	fmt.Println("✓ Авторизация успешна! Начинаю рассылку откликов...")

	if !s.opts.AlwaysVisible {
		s.setWindowVisible(page, false)
		fmt.Println("  Браузер свёрнут. Он появится только при вакансиях с вопросами.")
	}

	filters, err := s.repo.GetActiveFilters()
	if err != nil {
		return fmt.Errorf("получение фильтров: %w", err)
	}
	if len(filters) == 0 {
		fmt.Println("Нет активных фильтров. Добавьте: hh_spammer add-filter <url> [название]")
		return nil
	}

	for _, f := range filters {
		label := f.Name
		if label == "" {
			label = f.URL
		}
		fmt.Printf("\n=== Фильтр: %s ===\n", label)
		if err := s.processFilter(page, f.URL); err != nil {
			log.Printf("Ошибка фильтра [%s]: %v\n", label, err)
		}
	}

	total, _ := s.repo.CountApplied()
	fmt.Printf("\n✓ Готово! Всего успешных откликов в базе: %d\n", total)
	return nil
}

func (s *Spammer) waitForLogin(page *rod.Page) error {
	loginPages := []string{
		"/account/login",
		"/account/signup",
		"/oauth",
		"account/connect",
	}

	fmt.Print("  Ожидание")
	for i := 0; i < 150; i++ {
		time.Sleep(2 * time.Second)
		info, err := page.Info()
		if err != nil {
			fmt.Print(".")
			continue
		}
		u := info.URL

		if !strings.Contains(u, "hh.ru") {
			fmt.Print(".")
			continue
		}

		onLoginPage := false
		for _, p := range loginPages {
			if strings.Contains(u, p) {
				onLoginPage = true
				break
			}
		}
		if onLoginPage {
			fmt.Print(".")
			continue
		}

		authSelectors := []string{
			"[data-qa='mainmenu_myResumes']",
			"[data-qa='mainmenu_myNegotiations']",
			"[data-qa='header-user-menu']",
			".supernova-navi-item_candidate",
			"[data-hh-noindex='true']",
		}
		for _, sel := range authSelectors {
			if _, err := page.Timeout(2 * time.Second).Element(sel); err == nil {
				fmt.Println(" вошли!")
				return nil
			}
		}

		if strings.Contains(u, "hh.ru/") && !strings.Contains(u, "hh.ru/account") {
			fmt.Println(" вошли!")
			return nil
		}
	}
	fmt.Println()
	return ErrLoginTimeout
}

// setWindowVisible сворачивает или разворачивает окно браузера.
// В режиме AlwaysVisible ничего не делает.
func (s *Spammer) setWindowVisible(page *rod.Page, visible bool) {
	if s.opts.AlwaysVisible {
		return
	}
	info, err := proto.BrowserGetWindowForTarget{TargetID: page.TargetID}.Call(page)
	if err != nil {
		return
	}
	state := proto.BrowserWindowStateMinimized
	if visible {
		state = proto.BrowserWindowStateNormal
	}
	_ = proto.BrowserSetWindowBounds{
		WindowID: info.WindowID,
		Bounds:   &proto.BrowserBounds{WindowState: state},
	}.Call(page)
}

func (s *Spammer) processFilter(page *rod.Page, filterURL string) error {
	pageNum := 0
	totalFound, totalApplied := 0, 0

	for {
		pageURL := withPage(filterURL, pageNum)

		if err := page.Navigate(pageURL); err != nil {
			return fmt.Errorf("навигация: %w", err)
		}
		if err := page.WaitLoad(); err != nil {
			return fmt.Errorf("загрузка страницы: %w", err)
		}
		time.Sleep(2 * time.Second)

		vacancies := s.extractVacancies(page)
		if len(vacancies) == 0 {
			if pageNum == 0 {
				fmt.Println("  Вакансии не найдены по данному фильтру")
			}
			break
		}

		_, nextErr := page.Timeout(3 * time.Second).Element("[data-qa='pager-next']")
		hasNext := nextErr == nil

		fmt.Printf("  Страница %d: найдено %d вакансий\n", pageNum+1, len(vacancies))
		totalFound += len(vacancies)

		for _, v := range vacancies {
			if s.repo.IsApplied(v.id) {
				log.Printf("  — Пропуск (уже откликался): %s — %s\n", v.company, v.title)
				continue
			}

			err := s.apply(page, v)

			entry := &db.Applied{
				VacancyID: v.id,
				URL:       v.vacURL,
				Title:     v.title,
				Company:   v.company,
				AppliedAt: time.Now(),
			}

			switch {
			case err == nil:
				fmt.Printf("  ✓ Отклик отправлен: %s — %s\n", v.company, v.title)
				entry.Success = true
				totalApplied++
				_ = s.repo.SaveApplied(entry)

			case errors.Is(err, ErrAlreadyApplied):
				log.Printf("  — Уже откликался (статус на сайте): %s — %s\n", v.company, v.title)
				entry.Success = true
				_ = s.repo.SaveApplied(entry)

			case errors.Is(err, ErrExternalVacancy):
				log.Printf("  — Внешняя вакансия, пропуск: %s — %s\n", v.company, v.title)
				continue

			case errors.Is(err, ErrQuestionsPage):
				if s.opts.SkipQuestions {
					log.Printf("  — Пропуск (вопросы при отклике): %s — %s\n", v.company, v.title)
					continue
				}
				// Разворачиваем браузер и ждём пока пользователь ответит
				s.setWindowVisible(page, true)
				fmt.Printf("  ❓ Вакансия с вопросами: %s — %s\n", v.company, v.title)
				fmt.Printf("     Ответьте на вопросы в браузере. Таймаут: %s\n", s.opts.QuestionTimeout)
				applied := s.waitForQuestionsComplete(page, v.vacURL)
				s.setWindowVisible(page, false)
				if applied {
					fmt.Printf("  ✓ Отклик с вопросами отправлен: %s — %s\n", v.company, v.title)
					entry.Success = true
					totalApplied++
					_ = s.repo.SaveApplied(entry)
				} else {
					log.Printf("  ✗ Таймаут вопросов, пропускаю: %s — %s\n", v.company, v.title)
				}
				continue

			default:
				log.Printf("  ✗ Ошибка: %s — %s: %v\n", v.company, v.title, err)
				entry.ErrorMsg = err.Error()
				_ = s.repo.SaveApplied(entry)
			}

			delay := s.cfg.DelayMin + rand.Intn(max(s.cfg.DelayMax-s.cfg.DelayMin, 1))
			fmt.Printf("  ⏳ Пауза %d сек...\n", delay)
			time.Sleep(time.Duration(delay) * time.Second)
		}

		if !hasNext {
			break
		}
		pageNum++
	}

	fmt.Printf("  Итого по фильтру: найдено %d, откликов %d\n", totalFound, totalApplied)
	return nil
}

// waitForQuestionsComplete ждёт пока пользователь ответит на вопросы и отправит отклик.
// Возвращает true если отклик успешно отправлен, false при таймауте.
func (s *Spammer) waitForQuestionsComplete(page *rod.Page, vacURL string) bool {
	deadline := time.Now().Add(s.opts.QuestionTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)

		// Проверяем — перешли ли на страницу "Резюме доставлено"
		if _, err := page.Timeout(time.Second).Element(".vacancy-actions_responded"); err == nil {
			// Если появилась форма добавления письма — заполняем её
			if _, err := page.Timeout(2 * time.Second).Element("[data-qa='vacancy-response-letter-informer']"); err == nil {
				sel := "[data-qa='vacancy-response-letter-informer'] textarea[name='text']"
				_ = s.fillTextareaBySelector(page, sel)
				time.Sleep(time.Second)
				if btn, err := page.Timeout(3 * time.Second).Element("[data-qa='vacancy-response-letter-submit']"); err == nil {
					_ = btn.Click(proto.InputMouseButtonLeft, 1)
					time.Sleep(2 * time.Second)
				}
			}
			return true
		}

		// Или вернулись на страницу вакансии со статусом "уже откликался"
		if _, err := page.Timeout(time.Second).Element("[data-qa='vacancy-response-letter-already-sent']"); err == nil {
			return true
		}
	}
	return false
}

type vacancyInfo struct {
	id      string
	vacURL  string
	title   string
	company string
}

func (s *Spammer) extractVacancies(page *rod.Page) []vacancyInfo {
	els, err := page.Elements("[data-qa='vacancy-serp__vacancy']")
	if err != nil || len(els) == 0 {
		return nil
	}

	var result []vacancyInfo
	for _, el := range els {
		titleEl, err := el.Element("[data-qa='serp-item__title']")
		if err != nil {
			continue
		}
		href, _ := titleEl.Attribute("href")
		if href == nil {
			continue
		}
		title, _ := titleEl.Text()

		company := ""
		if compEl, err := el.Element("[data-qa='vacancy-serp__vacancy-employer']"); err == nil {
			company, _ = compEl.Text()
		}

		vacURL := *href
		if !strings.HasPrefix(vacURL, "http") {
			vacURL = "https://hh.ru" + vacURL
		}
		if parsed, err := url.Parse(vacURL); err == nil {
			parsed.RawQuery = ""
			parsed.Fragment = ""
			vacURL = parsed.String()
		}

		result = append(result, vacancyInfo{
			id:      extractVacancyID(vacURL),
			vacURL:  vacURL,
			title:   strings.TrimSpace(title),
			company: strings.TrimSpace(company),
		})
	}
	return result
}

func (s *Spammer) apply(page *rod.Page, v vacancyInfo) error {
	if err := page.Navigate(v.vacURL); err != nil {
		return err
	}
	if err := page.WaitLoad(); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)

	// Already applied?
	if _, err := page.Timeout(2 * time.Second).Element("[data-qa='vacancy-response-letter-already-sent']"); err == nil {
		return ErrAlreadyApplied
	}
	if _, err := page.Timeout(1 * time.Second).Element(".vacancy-actions_responded"); err == nil {
		return ErrAlreadyApplied
	}

	// External vacancy?
	if el, err := page.Timeout(2 * time.Second).Element("[data-qa='vacancy-response-link-top']"); err == nil {
		if href, _ := el.Attribute("href"); href != nil && *href != "" &&
			!strings.HasPrefix(*href, "/") && !strings.Contains(*href, "hh.ru") {
			return ErrExternalVacancy
		}
	}

	// Click apply button
	applyBtn, err := page.Timeout(5 * time.Second).Element("[data-qa='vacancy-response-link-top']")
	if err != nil {
		applyBtn, err = page.Timeout(3 * time.Second).Element("[data-qa='vacancy-response-link-bottom']")
		if err != nil {
			return fmt.Errorf("кнопка «Откликнуться» не найдена")
		}
	}
	if err := applyBtn.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return fmt.Errorf("клик по кнопке: %w", err)
	}
	time.Sleep(3 * time.Second)

	// --- Сценарий A: попап с полем письма ---
	if s.cfg.ResumeTitle != "" {
		s.selectResume(page)
		time.Sleep(time.Second)
	}
	if _, err := page.Timeout(2 * time.Second).Element("[data-qa='vacancy-response-popup-form-letter-input']"); err == nil {
		if fillErr := s.fillTextareaBySelector(page, "[data-qa='vacancy-response-popup-form-letter-input']"); fillErr != nil {
			log.Printf("  Предупреждение: не удалось вставить письмо в попап: %v\n", fillErr)
		}
		time.Sleep(time.Second)
		for _, sel := range []string{
			"[data-qa='vacancy-response-letter-submit']",
			"[data-qa='vacancy-response-submit-popup']",
			"[data-qa='vacancy-response-popup-submit']",
		} {
			if btn, err := page.Timeout(3 * time.Second).Element(sel); err == nil {
				_ = btn.Click(proto.InputMouseButtonLeft, 1)
				time.Sleep(3 * time.Second)
				return nil
			}
		}
	}

	// --- Сценарий B: скролл вниз, форма с письмом на той же странице ---
	if _, err := page.Timeout(5 * time.Second).Element("[data-qa='vacancy-response-letter-informer']"); err == nil {
		sel := "[data-qa='vacancy-response-letter-informer'] textarea[name='text']"
		if fillErr := s.fillTextareaBySelector(page, sel); fillErr != nil {
			log.Printf("  Предупреждение: не удалось вставить письмо в форму после отклика: %v\n", fillErr)
		} else {
			time.Sleep(time.Second)
			if btn, err := page.Timeout(3 * time.Second).Element("[data-qa='vacancy-response-letter-submit']"); err == nil {
				if err := btn.Click(proto.InputMouseButtonLeft, 1); err != nil {
					return fmt.Errorf("клик по кнопке отправки письма: %w", err)
				}
				time.Sleep(2 * time.Second)
				if _, err := page.Timeout(2 * time.Second).Element("[data-qa='form-helper-error']"); err == nil {
					log.Println("  Ошибка валидации — пробую form.requestSubmit()")
					_, _ = page.Eval(`() => {
						const form = document.querySelector("form[action*='vacancy_response']");
						if (form) form.requestSubmit();
					}`)
					time.Sleep(2 * time.Second)
				}
			}
		}
		return nil
	}

	// --- Сценарий C: редирект на страницу с вопросами ---
	info, err := page.Info()
	if err == nil && !strings.Contains(info.URL, "/vacancy/") {
		return ErrQuestionsPage
	}

	return ErrQuestionsPage
}

// fillTextareaBySelector устанавливает значение textarea через нативный JS-сеттер,
// обходя React controlled input.
func (s *Spammer) fillTextareaBySelector(page *rod.Page, selector string) error {
	if el, err := page.Timeout(3 * time.Second).Element(selector); err == nil {
		_ = el.Focus()
		time.Sleep(300 * time.Millisecond)
	}
	_, err := page.Eval(`(sel, text) => {
		const el = document.querySelector(sel);
		if (!el) return false;
		const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value').set;
		setter.call(el, text);
		el.dispatchEvent(new Event('input',  { bubbles: true }));
		el.dispatchEvent(new Event('change', { bubbles: true }));
		el.dispatchEvent(new Event('blur',   { bubbles: true }));
		return true;
	}`, selector, s.cfg.CoverLetter)
	return err
}

func (s *Spammer) selectResume(page *rod.Page) {
	items, err := page.Elements("[data-qa='resume-title']")
	if err != nil {
		return
	}
	keyword := strings.ToLower(s.cfg.ResumeTitle)
	for _, item := range items {
		text, _ := item.Text()
		if strings.Contains(strings.ToLower(text), keyword) {
			_ = item.Click(proto.InputMouseButtonLeft, 1)
			return
		}
	}
}

func withPage(rawURL string, page int) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	if page == 0 {
		q.Del("page")
	} else {
		q.Set("page", fmt.Sprintf("%d", page))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func extractVacancyID(vacURL string) string {
	u, err := url.Parse(vacURL)
	if err != nil {
		return vacURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if p == "vacancy" && i+1 < len(parts) {
			id := parts[i+1]
			if idx := strings.IndexAny(id, "?#"); idx >= 0 {
				id = id[:idx]
			}
			return id
		}
	}
	return vacURL
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
