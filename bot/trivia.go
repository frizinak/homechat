package bot

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"math/rand"
	"strconv"
	"strings"
	"sync"
)

type triviaResult struct {
	Results results `json:"results"`
}

type results []result

type result struct {
	Type       string   `json:"type"`
	Category   string   `json:"category"`
	Question   string   `json:"question"`
	Difficulty string   `json:"difficulty"`
	Correct    string   `json:"correct_answer"`
	Incorrect  []string `json:"incorrect_answers"`

	List []string
}

func (r result) Value() int {
	switch r.Difficulty {
	case "hard":
		return 5
	case "medium":
		return 2
	}
	return 1
}

func (r result) Init() result {
	answers := make([]string, 1, len(r.Incorrect)+1)
	answers[0] = html.UnescapeString(r.Correct)
	for i := range r.Incorrect {
		answers = append(answers, html.UnescapeString(r.Incorrect[i]))
	}

	rand.Shuffle(len(answers), func(i, j int) {
		answers[i], answers[j] = answers[j], answers[i]
	})

	r.List = answers

	r.Type = html.UnescapeString(r.Type)
	r.Category = html.UnescapeString(r.Category)
	r.Question = html.UnescapeString(r.Question)
	r.Correct = html.UnescapeString(r.Correct)
	return r
}

func (r result) String() string {
	answers := make([]string, len(r.List))
	for i, a := range r.List {
		answers[i] = fmt.Sprintf("  %d) %s", i+1, a)
	}

	return fmt.Sprintf(
		"TRIVIA %s-%s [%s]\n  %s\n%s",
		r.Type,
		r.Difficulty,
		r.Category,
		r.Question,
		strings.Join(answers, "\n"),
	)
}

type TriviaBot struct {
	questions results
	sem       sync.Mutex
	scores    map[string]int
	answered  map[string]int
	current   map[string]result
}

const TriviaName = "trivia-bot"

func NewTriviaBot() *TriviaBot {
	return &TriviaBot{
		questions: make(results, 0),
		scores:    make(map[string]int),
		answered:  make(map[string]int),
		current:   make(map[string]result),
	}
}

func (t *TriviaBot) Message(user string, args ...string) (string, string, error) {
	t.sem.Lock()
	defer t.sem.Unlock()

	if len(t.questions) < 5 {
		_result, err := simpleAPI(
			"https://opentdb.com/api.php?amount=10",
			&triviaResult{},
		)

		result := _result.(*triviaResult)
		t.questions = append(t.questions, result.Results...)

		if err != nil {
			return TriviaName, err.Error(), err
		}
	}

	if len(args) != 0 && args[0] == "help" {
		b := bytes.NewBuffer(nil)
		fmt.Fprintln(b, " - <no argument>")
		fmt.Fprintln(b, "       get a new question")
		fmt.Fprintln(b, " - 0-n")
		fmt.Fprintln(b, "       answer last question")
		fmt.Fprintln(b, " - score")
		fmt.Fprintln(b, "       show your score")
		fmt.Fprintln(b, " - reset")
		fmt.Fprintln(b, "       reset everything")
		return TriviaName, b.String(), nil
	}

	score := func(buf io.Writer, user string) {
		s := t.scores[user]
		a := t.answered[user]
		var avg float64
		if a > 0 {
			avg = float64(s) / float64(a)
		}
		fmt.Fprintf(buf, "%s has %d points for answering %d questions. avg: %.2f", user, s, a, avg)
	}

	if len(args) != 0 && args[0] == "score" {
		buf := bytes.NewBuffer(nil)
		for user := range t.answered {
			score(buf, user)
		}
		return TriviaName, buf.String(), nil
	}

	if len(args) != 0 && args[0] == "reset" {
		t.scores = make(map[string]int)
		t.answered = make(map[string]int)
		t.current = make(map[string]result)
		return TriviaName, "everything was reset", nil
	}

	if _, ok := t.current[user]; !ok {
		t.current[user] = t.questions[0].Init()
		t.questions = t.questions[1:]
		return TriviaName, t.current[user].String(), nil
	}

	current := t.current[user]
	if len(args) != 1 {
		return TriviaName, current.String(), nil
	}

	choice, err := strconv.Atoi(args[0])
	if err != nil || choice < 1 || choice > len(current.List) {
		return TriviaName, "invalid answer", nil
	}

	answer := current.List[choice-1]
	buf := bytes.NewBuffer(nil)
	t.answered[user]++
	delete(t.current, user)
	if answer == current.Correct {
		fmt.Fprintln(buf, "CORRECT!")
		t.scores[user] += current.Value()
		score(buf, user)
		return TriviaName, buf.String(), nil
	}

	fmt.Fprintln(buf, "INCORRECT...")
	fmt.Fprintf(buf, "correct answer was '%s'", current.Correct)

	return TriviaName, buf.String(), nil
}
