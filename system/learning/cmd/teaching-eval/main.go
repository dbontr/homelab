package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/dbontr/Adaptive-Teaching-MCP/internal/teacheval"
)

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "teaching-eval:", err)
		os.Exit(1)
	}
}

func readSuite(path string) (teacheval.Suite, error) {
	var suite teacheval.Suite
	if path == "" {
		return suite, errors.New("-suite path is required")
	}
	err := teacheval.ReadJSON(path, &suite)
	if err != nil {
		return suite, err
	}
	return suite, teacheval.ValidateSuite(suite)
}

func readRubrics(path string) (teacheval.Rubrics, error) {
	var r teacheval.Rubrics
	if path == "" {
		return r, errors.New("-rubrics path is required")
	}
	err := teacheval.ReadJSON(path, &r)
	return r, err
}

func execute(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: teaching-eval validate|run|blind|report [flags]")
	}
	switch args[0] {
	case "validate":
		f := flag.NewFlagSet("validate", flag.ContinueOnError)
		suiteFile := f.String("suite", "", "learner script JSON")
		rubricFile := f.String("rubrics", "", "separate evaluation rubrics")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() > 0 {
			return errors.New("unexpected positional arguments")
		}
		suite, err := readSuite(*suiteFile)
		if err != nil {
			return err
		}
		rubrics, err := readRubrics(*rubricFile)
		if err != nil {
			return err
		}
		if err := teacheval.ValidateRubrics(suite, rubrics); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Evaluation schema valid: %d cases.\n", len(suite.Cases))
		return nil
	case "run":
		f := flag.NewFlagSet("run", flag.ContinueOnError)
		suiteFile := f.String("suite", "", "learner script JSON")
		policyFile := f.String("policy", "", "system teaching policy; keep separate from hidden evaluation criteria")
		condition := f.String("condition", "", "condition identifier, e.g. baseline or foundation")
		endpoint := f.String("endpoint", "", "OpenAI-compatible chat-completions URL")
		model := f.String("model", "", "model identifier")
		keyEnv := f.String("key-env", "TEACHING_EVAL_API_KEY", "optional name of a local API token environment variable")
		output := f.String("out", "", "exclusive 0600 run transcript path")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() > 0 || *output == "" || *policyFile == "" {
			return errors.New("-policy and -out are required")
		}
		if err := teacheval.ValidateEndpoint(*endpoint); err != nil {
			return err
		}
		suite, err := readSuite(*suiteFile)
		if err != nil {
			return err
		}
		info, err := os.Stat(*policyFile)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 75000 {
			return errors.New("policy must be a regular file smaller than 75 kB")
		}
		policy, err := os.ReadFile(*policyFile)
		if err != nil {
			return err
		}
		apiKey := ""
		if *keyEnv != "" {
			apiKey = os.Getenv(*keyEnv)
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
		defer cancel()
		run, runErr := teacheval.RunEvaluation(ctx, suite, string(policy), *condition, *model, teacheval.HTTPClient{
			Endpoint: *endpoint, APIKey: apiKey,
		})
		if err := teacheval.WritePrivate(*output, run); err != nil {
			return err
		}
		if runErr != nil {
			return fmt.Errorf("partial transcript saved: %w", runErr)
		}
		completed := 0
		for _, a := range run.Attempts {
			if a.Error == "" {
				completed++
			}
		}
		fmt.Fprintf(os.Stdout, "Saved %d completed transcripts of %d cases. Outputs are model behavior observations, not learner-outcome measurements.\n", completed, len(run.Attempts))
		return nil
	case "blind":
		f := flag.NewFlagSet("blind", flag.ContinueOnError)
		suiteFile := f.String("suite", "", "learner script JSON")
		rubricFile := f.String("rubrics", "", "private case-specific rubrics")
		runFiles := f.String("runs", "", "comma-separated completed run files for comparison")
		output := f.String("out", "", "exclusive 0600 blinded reviewer bundle")
		keyPath := f.String("key", "", "exclusive 0600 private unblinding map")
		templatePath := f.String("template", "", "optional blank double-reviewer template")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() > 0 || *output == "" || *keyPath == "" || *output == *keyPath || *templatePath == *output || *templatePath == *keyPath {
			return errors.New("distinct -out, -key and optional -template paths are required")
		}
		suite, err := readSuite(*suiteFile)
		if err != nil {
			return err
		}
		rubrics, err := readRubrics(*rubricFile)
		if err != nil {
			return err
		}
		paths := strings.Split(*runFiles, ",")
		if len(paths) < 2 {
			return errors.New("at least two runs are required")
		}
		runs := make([]teacheval.Run, 0, len(paths))
		for _, path := range paths {
			var run teacheval.Run
			if err := teacheval.ReadJSON(strings.TrimSpace(path), &run); err != nil {
				return err
			}
			runs = append(runs, run)
		}
		bundle, key, err := teacheval.Blind(suite, rubrics, runs)
		if err != nil {
			return err
		}
		if err = teacheval.WritePrivate(*output, bundle); err != nil {
			return err
		}
		if err = teacheval.WritePrivate(*keyPath, key); err != nil {
			os.Remove(*output)
			return err
		}
		if *templatePath != "" {
			template := teacheval.Reviews{Version: teacheval.Version, Ratings: []teacheval.Rating{}}
			for _, s := range bundle.Samples {
				for _, reviewer := range []string{"reviewer-one", "reviewer-two"} {
					rating := teacheval.Rating{BlindID: s.BlindID, ReviewerID: reviewer, Dimensions: []teacheval.DimensionReview{}}
					for _, d := range teacheval.Dimensions {
						rating.Dimensions = append(rating.Dimensions, teacheval.DimensionReview{Dimension: d, Score: -1})
					}
					template.Ratings = append(template.Ratings, rating)
				}
			}
			if err = teacheval.WritePrivate(*templatePath, template); err != nil {
				os.Remove(*output)
				os.Remove(*keyPath)
				return err
			}
		}
		fmt.Fprintf(os.Stdout, "Prepared %d blinded model transcripts. Keep the private map away from independent reviewers.\n", len(bundle.Samples))
		return nil
	case "report":
		f := flag.NewFlagSet("report", flag.ContinueOnError)
		bundlePath := f.String("bundle", "", "blinded reviewer bundle")
		keyPath := f.String("key", "", "private identity mapping")
		reviewsPath := f.String("reviews", "", "human reviewer ratings")
		output := f.String("out", "", "exclusive 0600 report destination")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		if f.NArg() > 0 || *output == "" {
			return errors.New("-out is required")
		}
		var bundle teacheval.BlindBundle
		var key teacheval.BlindKey
		var reviews teacheval.Reviews
		for _, entry := range []struct {
			path  string
			value any
		}{{*bundlePath, &bundle}, {*keyPath, &key}, {*reviewsPath, &reviews}} {
			if err := teacheval.ReadJSON(entry.path, entry.value); err != nil {
				return err
			}
		}
		report, err := teacheval.ScoreBlinded(bundle, key, reviews)
		if err != nil {
			return err
		}
		if err := teacheval.WritePrivate(*output, report); err != nil {
			return err
		}
		fmt.Fprintf(os.Stdout, "Review status: %s; %d matched condition pairs; %d unresolved dispute cases.\n", report.Status, report.MatchedPairs, report.DisputedSamples)
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}
