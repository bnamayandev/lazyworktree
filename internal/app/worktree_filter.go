package app

import (
	"strings"

	"github.com/chmouel/lazyworktree/internal/models"
)

type worktreeFilterQuery struct {
	textTerms []string
	tagTerms  []string
}

func parseWorktreeFilterQuery(query string) worktreeFilterQuery {
	var parsed worktreeFilterQuery
	for _, token := range strings.Fields(strings.ToLower(strings.TrimSpace(query))) {
		if strings.HasPrefix(token, "tag:") {
			tag := strings.TrimSpace(strings.TrimPrefix(token, "tag:"))
			if tag != "" {
				parsed.tagTerms = append(parsed.tagTerms, tag)
			}
			continue
		}
		parsed.textTerms = append(parsed.textTerms, token)
	}
	return parsed
}

func worktreeMatchesFilter(wt *models.WorktreeInfo, note models.WorktreeNote, hasNote bool, parsed worktreeFilterQuery) bool {
	if len(parsed.textTerms) == 0 && len(parsed.tagTerms) == 0 {
		return true
	}

	tagSet := make(map[string]struct{}, len(note.Tags))
	if hasNote {
		for _, tag := range note.Tags {
			tagSet[strings.ToLower(tag)] = struct{}{}
		}
	}
	for _, tag := range parsed.tagTerms {
		if _, ok := tagSet[tag]; !ok {
			return false
		}
	}

	name := worktreeDisplayName(wt)

	baseHaystacks := []string{strings.ToLower(name), strings.ToLower(wt.Branch)}
	if hasNote {
		if note.Description != "" {
			baseHaystacks = append(baseHaystacks, strings.ToLower(note.Description))
		}
		if len(note.Tags) > 0 {
			baseHaystacks = append(baseHaystacks, strings.ToLower(strings.Join(note.Tags, " ")))
		}
	}

	for _, term := range parsed.textTerms {
		haystacks := baseHaystacks
		if strings.Contains(term, "/") {
			haystacks = append(haystacks, strings.ToLower(wt.Path))
		}

		matched := false
		for _, haystack := range haystacks {
			if strings.Contains(haystack, term) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	return true
}
