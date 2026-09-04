// Package chat ведёт разговор: собирает историю, зовёт модель, исполняет
// инструменты и записывает чек.
//
// Чек — не побочный журнал, а обязательство: каждый ответ бота должен уметь
// объяснить, откуда он взялся. Поэтому шаги пишутся по ходу, а не собираются
// задним числом из логов.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dripips/hark/internal/lang"
	"github.com/dripips/hark/internal/llm"
	"github.com/dripips/hark/internal/store"
	"github.com/dripips/hark/internal/tools"
)

// escalateTool — встроенный инструмент, которым бот зовёт человека. Отдельный
// инструмент лучше, чем угадывание по тексту: модель говорит о сдаче прямо, и
// причина попадает в очередь менеджера.
const escalateTool = "call_human"

// maxTurns ограничивает цикл «модель просит инструмент». Без потолка модель,
// у которой инструмент всё время отказывает, будет ходить по кругу за деньги.
const maxTurns = 6

type Engine struct {
	DB *store.DB
}

// Reply — итог одного ответа бота.
type Reply struct {
	Message   *store.Message
	Receipt   *store.Receipt
	Escalated bool
	Reason    string
}

// OnChunk получает куски текста по мере готовности. Может быть nil.
type OnChunk func(text string)

// Answer отвечает на последнюю реплику посетителя.
func (e *Engine) Answer(ctx context.Context, bot *store.Bot, conv *store.Conversation,
	onChunk OnChunk) (*Reply, error) {

	started := time.Now()
	receipt := &store.Receipt{BotID: bot.ID, Provider: bot.Provider, Model: bot.Model}

	provider, err := buildProvider(bot)
	if err != nil {
		return nil, err
	}

	// База живёт по своему контексту, оторванному от запроса.
	//
	// Посетитель может закрыть вкладку в любую секунду разговора, и контекст
	// запроса при этом отменяется. Обращение к модели и к подключениям это
	// действительно должно прерывать — платить за ответ, который никто не
	// прочтёт, незачем. А вот запись прерывать нельзя: иначе реплика «Передаю
	// разговор менеджеру» остаётся у посетителя, но разговор не попадает в
	// очередь, и человека, которого пообещали, никто не зовёт.
	dbCtx, closeDB := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer closeDB()

	toolRows, err := e.DB.Tools(dbCtx, bot.ID)
	if err != nil {
		return nil, err
	}
	runners := map[string]tools.Runner{}
	specs := []llm.Tool{{
		Name: escalateTool,
		Description: lang.T(bot.LangOr(),
			"Позвать живого менеджера, когда не хватает данных или прав. "+
				"Причину напиши коротко и по делу."),
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"reason": map[string]any{
					"type":        "string",
					"description": lang.T(bot.LangOr(), "почему нужен человек"),
				},
			},
			"required": []string{"reason"},
		},
	}}
	for _, row := range toolRows {
		if !row.Enabled {
			continue
		}
		runner, err := tools.Build(row)
		if err != nil {
			// Сломанный инструмент не должен ронять разговор, но и молчать о
			// нём нельзя: он попадает в чек отдельным шагом.
			receipt.Steps = append(receipt.Steps, store.Step{
				Kind: "error", Name: row.Name, Detail: err.Error(),
			})
			continue
		}
		runners[row.Name] = runner
		specs = append(specs, llm.Tool{
			Name: row.Name, Description: row.Description, Parameters: tools.Schema(row),
		})
	}

	history, err := e.buildHistory(dbCtx, bot, conv)
	if err != nil {
		return nil, err
	}

	var answer string
	var escalated bool
	var reason string
	usage := llm.Usage{}

	for turn := 0; turn < maxTurns; turn++ {
		req := llm.Request{
			Model:     bot.Model,
			Messages:  history,
			Tools:     specs,
			MaxTokens: bot.MaxTokens,
		}
		applyKnobs(bot, &req)

		resp, err := runTurn(ctx, provider, req, onChunk)
		if err != nil {
			receipt.Steps = append(receipt.Steps, store.Step{
				Kind: "error", Name: bot.Model, Detail: err.Error(),
			})
			receipt.Error = err.Error()
			break
		}

		usage.PromptTokens += resp.Usage.PromptTokens
		usage.CompletionTokens += resp.Usage.CompletionTokens
		usage.ReasoningTokens += resp.Usage.ReasoningTokens
		usage.CachedTokens += resp.Usage.CachedTokens
		if resp.Model != "" {
			receipt.Model = resp.Model
		}

		step := store.Step{
			Kind:   "model",
			Name:   receipt.Model,
			Status: resp.FinishedAs,
			TookMS: resp.Took.Milliseconds(),
			Detail: fmt.Sprintf("ввод %d, вывод %d, рассуждение %d",
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.ReasoningTokens),
		}
		if len(resp.ToolCalls) > 0 {
			names := make([]string, 0, len(resp.ToolCalls))
			for _, call := range resp.ToolCalls {
				names = append(names, call.Name)
			}
			step.Response = "просит инструменты: " + strings.Join(names, ", ")
		} else {
			step.Response = truncate(resp.Text, 400)
		}
		receipt.Steps = append(receipt.Steps, step)

		if len(resp.ToolCalls) == 0 {
			answer = strings.TrimSpace(resp.Text)
			break
		}

		history = append(history, llm.Message{
			Role: llm.RoleAssistant, Text: resp.Text, ToolCalls: resp.ToolCalls,
		})

		for _, call := range resp.ToolCalls {
			args := map[string]any{}
			_ = json.Unmarshal([]byte(call.Arguments), &args)

			if call.Name == escalateTool {
				escalated = true
				reason, _ = args["reason"].(string)
				if reason == "" {
					reason = "бот не смог ответить"
				}
				receipt.Steps = append(receipt.Steps, store.Step{
					Kind: "tool", Name: escalateTool, Detail: reason, Status: "зовём человека",
				})
				history = append(history, llm.Message{
					Role: llm.RoleTool, ToolCallID: call.ID,
					Text: lang.T(bot.LangOr(),
						"Менеджер вызван. Скажи посетителю, что человек подключится, и не обещай сроков."),
				})
				continue
			}

			runner, ok := runners[call.Name]
			if !ok {
				receipt.Steps = append(receipt.Steps, store.Step{
					Kind: "error", Name: call.Name, Detail: "инструмента нет",
				})
				history = append(history, llm.Message{
					Role: llm.RoleTool, ToolCallID: call.ID,
					Text: "Инструмент недоступен.",
				})
				continue
			}

			result := runner.Run(ctx, args)
			status := result.Status
			text := result.Text
			if result.Err != nil {
				status = "ошибка"
				text = "Инструмент не ответил: " + result.Err.Error()
			}
			receipt.Steps = append(receipt.Steps, store.Step{
				Kind:     "tool",
				Name:     call.Name,
				Request:  truncate(result.Request, 900),
				Response: truncate(result.Response, 900),
				Status:   status,
				TookMS:   result.Took.Milliseconds(),
			})
			history = append(history, llm.Message{
				Role: llm.RoleTool, ToolCallID: call.ID, Text: text,
			})
		}
	}

	if answer == "" && !escalated {
		answer = lang.T(bot.LangOr(), "Не получилось ответить. Передаю разговор менеджеру.")
		escalated = true
		if reason == "" {
			reason = receipt.Error
			if reason == "" {
				reason = "бот не дал ответа"
			}
		}
	}
	if escalated && answer == "" {
		answer = lang.T(bot.LangOr(), "Подключаю менеджера, он ответит здесь же.")
	}

	receipt.PromptTokens = usage.PromptTokens
	receipt.CompletionTokens = usage.CompletionTokens
	receipt.ReasoningTokens = usage.ReasoningTokens
	receipt.CachedTokens = usage.CachedTokens
	receipt.CostMicro = Cost(bot, usage)
	receipt.TookMS = time.Since(started).Milliseconds()

	// Три записи, которые должны случиться все: реплика, чек к ней и, если бот
	// сдался, перевод разговора в очередь к человеку.
	message := &store.Message{ConversationID: conv.ID, Role: "assistant", Text: answer}
	if err := e.DB.AddMessage(dbCtx, message); err != nil {
		return nil, err
	}
	receipt.MessageID = message.ID
	if err := e.DB.SaveReceipt(dbCtx, receipt); err != nil {
		// Чек — не причина терять эскалацию: посетитель уже получил ответ.
		log.Printf("чек разговора %d не записан: %v", conv.ID, err)
	}

	if escalated {
		if err := e.DB.SetConversationState(dbCtx, conv.ID, "waiting", reason); err != nil {
			// Идти дальше некуда. Громкая строка в журнале — единственное, чем
			// мы можем предупредить владельца, что этот разговор ждёт человека,
			// которого не позвали.
			log.Printf("ВНИМАНИЕ: разговор %d не поставлен в очередь к человеку: %v", conv.ID, err)
		}
	}

	return &Reply{Message: message, Receipt: receipt, Escalated: escalated, Reason: reason}, nil
}

// Cost считает стоимость в микрорублях — миллионных долях рубля.
//
// Цена задаётся в копейках за миллион токенов, как её публикуют поставщики.
// Один ответ стоит доли копейки, поэтому копейка как единица хранения не
// годится: всё округлялось бы в ноль. Микрорубль держит и один ответ, и
// месячный итог одним целым числом.
//
// Токены рассуждения оплачиваются как вывод: их не видно в ответе, но за них
// выставляют счёт.
func Cost(bot *store.Bot, usage llm.Usage) int64 {
	in := int64(usage.PromptTokens) * bot.PriceIn / 100
	out := int64(usage.CompletionTokens) * bot.PriceOut / 100
	return in + out
}

// applyKnobs передаёт только те ручки, которые модель приняла на пробе.
// Иначе один параметр роняет весь разговор: думающие модели OpenAI отвергают
// любую температуру, кроме значения по умолчанию.
func applyKnobs(bot *store.Bot, req *llm.Request) {
	caps := bot.Caps()
	if bot.Temperature != "" {
		if allowed, ok := caps["Temperature"].(bool); ok && !allowed {
			// проба сказала «нет» — не передаём
		} else {
			var value float64
			if _, err := fmt.Sscanf(bot.Temperature, "%f", &value); err == nil {
				req.Temperature = &value
			}
		}
	}
	if bot.Reasoning != "" {
		if allowed, ok := caps["ReasoningEffort"].(bool); !ok || allowed {
			req.ReasoningEffort = bot.Reasoning
		}
	}
}

func runTurn(ctx context.Context, provider llm.Provider, req llm.Request,
	onChunk OnChunk) (*llm.Response, error) {

	if onChunk == nil {
		return provider.Complete(ctx, req)
	}
	stream, err := provider.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	for chunk := range stream {
		if chunk.Err != nil {
			return nil, chunk.Err
		}
		if chunk.Text != "" {
			onChunk(chunk.Text)
		}
		if chunk.Done {
			if chunk.Response == nil {
				return nil, fmt.Errorf("поток закончился без ответа")
			}
			return chunk.Response, nil
		}
	}
	return nil, fmt.Errorf("поток оборвался")
}

func (e *Engine) buildHistory(ctx context.Context, bot *store.Bot,
	conv *store.Conversation) ([]llm.Message, error) {

	rows, err := e.DB.Messages(ctx, conv.ID)
	if err != nil {
		return nil, err
	}
	history := []llm.Message{{Role: llm.RoleSystem, Text: systemPrompt(bot)}}
	for _, row := range rows {
		switch row.Role {
		case "user":
			history = append(history, llm.Message{Role: llm.RoleUser, Text: row.Text})
		case "assistant", "human":
			// Реплику менеджера модель видит как свою: для разговора важно,
			// что это было сказано «со стороны поддержки».
			history = append(history, llm.Message{Role: llm.RoleAssistant, Text: row.Text})
		}
	}
	return history, nil
}

func systemPrompt(bot *store.Bot) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(bot.Instructions))
	// Правила пишутся на языке бота. Иначе модель отвечает на языке правил, а
	// не посетителя: русское «Отвечай коротко» тянет русский ответ даже там,
	// где весь сайт английский.
	code := bot.LangOr()
	b.WriteString("\n\n" + lang.T(code, "Правила:") + "\n")
	b.WriteString("— " + lang.T(code, "Отвечай коротко и по делу.") + "\n")
	b.WriteString("— " + lang.T(code,
		"Данные бери только из инструментов. Не выдумывай номера, сроки и цены.") + "\n")
	b.WriteString("— " + lang.T(code,
		"Если данных не хватает или нужно решение человека, вызови %s вместо догадки.",
		escalateTool) + "\n")
	return b.String()
}

// buildProvider — переменная, а не функция: тесты подменяют её заглушкой и
// проверяют сборку чека без обращения к сети.
var buildProvider = func(bot *store.Bot) (llm.Provider, error) {
	switch bot.Provider {
	case "anthropic":
		return llm.NewAnthropic(bot.BaseURL, bot.APIKey), nil
	case "openai", "":
		return llm.NewOpenAI(bot.BaseURL, bot.APIKey), nil
	default:
		return nil, fmt.Errorf("неизвестный поставщик: %q", bot.Provider)
	}
}

// truncate режет по символам: чек и запрос могут быть на любом языке, а
// байтовая обрезка на кириллице врёт вдвое и рубит букву пополам.
func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "…"
}
