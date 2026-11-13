package store

import "testing"

func TestSaveModelsAndReadBack(t *testing.T) {
	s := openTemp(t)
	if err := s.SaveModels("deepseek", []string{"deepseek-chat", "deepseek-reasoner"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Models()
	if err != nil {
		t.Fatal(err)
	}
	if len(got["deepseek"]) != 2 {
		t.Errorf("Models = %v", got)
	}
}

func TestSaveModelsReplacesPreviousList(t *testing.T) {
	s := openTemp(t)
	if err := s.SaveModels("deepseek", []string{"deepseek-chat", "deepseek-reasoner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveModels("deepseek", []string{"deepseek-chat"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Models()
	if err != nil {
		t.Fatal(err)
	}
	if len(got["deepseek"]) != 1 {
		t.Errorf("expected 1 model after replacing, got %v", got["deepseek"])
	}
}

func TestSaveModelsKeepsPreviousListOnEmpty(t *testing.T) {
	s := openTemp(t)
	if err := s.SaveModels("ds", []string{"deepseek-chat", "deepseek-reasoner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveModels("ds", nil); err != nil {
		t.Fatal(err)
	}
	got, err := s.Models()
	if err != nil {
		t.Fatal(err)
	}
	if len(got["ds"]) != 2 {
		t.Errorf("an empty list must not delete the stored one, got %v", got["ds"])
	}
}

func TestModelsGroupsByProvider(t *testing.T) {
	s := openTemp(t)
	if err := s.SaveModels("deepseek", []string{"deepseek-chat"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveModels("openrouter", []string{"gpt-4o"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Models()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got["deepseek"]) != 1 || len(got["openrouter"]) != 1 {
		t.Errorf("Models = %v", got)
	}
}
