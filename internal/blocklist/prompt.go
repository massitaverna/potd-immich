package blocklist

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	promptFormat = "Add %s to blocklist? (y/N): "
	answerY      = "y"
	answerYes    = "yes"
	answerN      = "n"
	answerNo     = "no"
	answerEmpty  = ""

	invalidAnswerHint = "Please answer y or n."
)

var errNoAnswer = errors.New("no answer on stdin (run with an interactive terminal)")

// Prompter asks on out and reads one answer line per question from in.
// "y"/"yes" block, "n"/"no" or a bare Enter allow (any case); anything else asks again.
func Prompter(in io.Reader, out io.Writer) Confirm {
	// One scanner for all questions: it reads ahead, so a new one per question would drop buffered answers.
	scanner := bufio.NewScanner(in)
	promptAndScan := func(label string) (string, error) {
		fmt.Fprintf(out, promptFormat, label)
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", errNoAnswer
		}
		answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
		return answer, nil
	}

	return func(label string) (bool, error) {
		for {
			answer, err := promptAndScan(label)
			if err != nil {
				return false, err
			}

			switch answer {
			case answerY, answerYes:
				return true, nil
			case answerN, answerNo, answerEmpty:
				return false, nil
			default:
				fmt.Fprintln(out, invalidAnswerHint)
			}
		}
	}
}
