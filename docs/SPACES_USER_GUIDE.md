# Spaces User Guide

**What are Spaces?** Your personal knowledge experts that automatically improve your code.

---

## Quick Start

### What You Get

When you ask the AI to write code, **Spaces work behind the scenes** to:
- ✅ Apply proven patterns from your knowledge base
- ✅ Catch common mistakes before you see them
- ✅ Suggest the right approach for each task
- ✅ Learn from your feedback to get better over time

**No setup required.** Spaces activate automatically when you have knowledge bases configured.

---

## How Spaces Work

### 1. Automatic Expert Selection

The AI analyzes your request and picks the most relevant Spaces:

```
You: "implement user authentication"
🤖 AI uses: go-best-practices, security-guidelines
```

```
You: "fix this test failure"
🤖 AI uses: testing-patterns, debugging-checklist
```

**Task Types Recognized:**
- **Implementation**: Building new features
- **Debugging**: Finding and fixing bugs
- **Testing**: Writing or updating tests
- **Refactoring**: Improving existing code
- **Documentation**: Writing docs or comments
- **Architecture**: Design and structure decisions

### 2. Code Validation

Generated code is automatically checked against Space rules:

**Before (without Spaces):**
```go
// Code might have issues
func Process(data string) string {
    result := transform(data)  // What if this panics?
    return result
}
```

**After (with Spaces):**
```go
// Space: go-best-practices applied
func Process(data string) (string, error) {
    result, err := transform(data)
    if err != nil {
        return "", fmt.Errorf("transform failed: %w", err)
    }
    return result, nil
}
```

### 3. Automatic Revision

If validation finds issues, the AI **fixes them automatically** (up to 2 attempts):

```
🤖 Generated code
⚠️  Space found 3 violations: missing error handling
🔧 Revising code...
✅ Fixed! All Space rules satisfied
```

You get the corrected code on the first try.

---

## Subscriber-Only: Import a KB JSON File into a Space

This workflow is available to subscribed users who want to bring their own curated expertise into Spaces.

### When to Use It

Use this flow when you want to:
- Seed a Space with your own coding standards, architecture guidance, or product playbooks
- Import a curated Knowledge Base export that was generated from Markdown or another source
- Make that knowledge available to the copilot during coding, review, and research tasks

### Preparing Your Knowledge Base

**Step 1: Author Curated Markdown**

Create a structured Markdown file using the Item block format:

```markdown
# Coding Best Practices
## Go

- Item: Use structured error handling
  Summary: Return errors explicitly and wrap with context.
  Summary: Use fmt.Errorf with %w for error wrapping.
  Body:
  Go does not use exceptions. Return errors explicitly,
  check them early, and wrap them with context using
  fmt.Errorf with %w. Never ignore errors with _.
  Sources: https://go.dev/doc/effective_go
```

**Step 2: Compile to Self-Contained JSON Bundle**

Use the Knowledge Base Compiler to convert your Markdown into a portable JSON bundle:

```bash
go run ./lib/sop/ai/cmd/knowledge_compiler/main.go -input my_kb.md
```

The compiler automatically:
- ✅ Extracts categories from headings (# ## ###)
- ✅ Parses Item blocks into retrievable knowledge items
- ✅ Discovers referenced documents from Sources fields
- ✅ Embeds full document content into the bundle
- ✅ Creates a self-contained JSON file ready for import

**What Gets Bundled:**
- All Item blocks as retrievable knowledge
- Full content of any .md files referenced in Sources
- Category hierarchy and metadata
- Everything needed for offline use

**Result:** One `.json` file that works anywhere, no external dependencies.

### Import Steps

1. Open the Data Manager or the Space view in your app.
2. Create a new Space or select an existing one as the destination.
3. Click the **Import JSON** action.
4. Choose the Knowledge Base JSON file (compiled bundle).
5. Confirm the import and wait for the Space contents to load.
6. Start a coding or research workflow and let the copilot use the imported expertise.

**What Happens During Import:**
- Items are indexed for semantic search (3-6 items per query)
- Referenced documents are stored in the Space's document store
- Categories are organized for efficient retrieval
- Everything works offline once imported

### Best Practices

**For Markdown Authoring:**
- Keep Items focused on one concept each
- Use 1-5 Summary lines per Item (for semantic matching)
- Put detailed examples in the Body
- Reference supporting docs in Sources (they'll be auto-bundled)

**For Bundle Compilation:**
- Run compiler after updating source Markdown
- Test bundles in a small Space before broad distribution
- Keep category depth to 2-3 levels for best retrieval

**For Space Import:**
- Keep imported KB focused on a clear domain
- Re-import updated JSON after revising source
- Give feedback to tune Space effectiveness

### Note for Subscribers

This import path is part of the premium experience because it enables organization-specific and domain-specific expert knowledge to become first-class context for coding workflows. Built-in Spaces still work for general use, but importing your own KB JSON is how you turn curated guidance into reusable expertise inside a Space.

---

## Giving Feedback

Help Spaces learn what works for you:

### Feedback Buttons

When Spaces are used, you'll see:

```
Space Used: go-best-practices
👍 Helpful  👎 Needs Work
```

**👍 Click "Helpful" when:**
- Code works correctly on first try
- Patterns match your team's style
- Error handling is appropriate
- Tests are well-structured

**👎 Click "Needs Work" when:**
- Code doesn't match your requirements
- Patterns feel too strict or too loose
- Better approach should have been used
- Validation missed real issues

### Why Feedback Matters

Your feedback trains the effectiveness tracker:
- ✅ **Good feedback** → Space used more often for similar tasks
- ❌ **Negative feedback** → Space used less often, alternatives tried
- 📊 **Patterns emerge** → System learns your preferences over time

**Privacy Note:** Feedback is stored locally on your device. No code is sent to external servers.

---

## Understanding Explanations

Click the "How I used expertise" section to see:

### What Spaces Were Used
```
## Knowledge Bases Selected
I selected 2 Spaces: go-best-practices and testing-patterns
```

### Why They Were Chosen
```
## How I Approached This Task
I classified this as an implementation task.
Based on your file types (.go) and keywords like "implement"...
```

### What Changed
```
## How Spaces Influenced the Code
- Added error handling (go-best-practices)
- Used table-driven tests (testing-patterns)
- Applied naming conventions (go-best-practices)
```

### Quality Score
```
## Validation Results
✅ The generated code has 0 Space rule violation(s)
Compliance: 100% (all rules satisfied)
```

---

## Advanced Usage

### Importing a KB JSON File onto a Space

If you have a curated Knowledge Base export, you can import it into a Space so the copilot can use it as domain expertise during coding, research, and review tasks.

#### Categorizer support in phase 1

Phase 1 now preserves and rehydrates a Space's categorizer policy during JSON import/export. If the exported KB config includes a `categorizer_config` block, the imported Space restores that policy and uses it for routing and retrieval.

For the planned v1 general-purpose categorizer design, see [CATEGORIZER_V1_DESIGN_PLAN.md](CATEGORIZER_V1_DESIGN_PLAN.md). The v1 direction is an in-process, taxonomy-aware categorizer that uses embeddings plus category structure rather than an external dependency.

This is intended for domain-specific Spaces such as the coding best-practices KB:
- rule-based mappings can route queries into the right category branches,
- the imported Space can use the same routing policy during research and Copilot consumption,
- and the behavior is backward-compatible with older JSON exports that do not include categorizer config.

Example shape:

```json
{
  "config": {
    "categorizer_config": {
      "type": "rule",
      "default": {
        "categories": [
          { "path": "Coding Best Practices/Go", "keywords": ["go", "golang", "context"] }
        ]
      },
      "fallback": "uncategorized"
    }
  }
}
```

**Complete Workflow:**

1. **Author:** Create structured Markdown with Item blocks
2. **Compile:** Run Knowledge Base Compiler to generate self-contained JSON
   ```bash
   go run ./lib/sop/ai/cmd/knowledge_compiler/main.go -input my_kb.md
   ```
3. **Import:** Open Space view, choose Import JSON, select the bundle
4. **Use:** Space automatically applies during AI-assisted coding

**Bundle Contents:**
- **Items:** Retrievable knowledge (3-6 per query via semantic search)
- **Documents:** Full source docs for detailed context on demand
- **Categories:** Organized hierarchy for efficient navigation
- **Categorizer policy (phase 1):** Optional routing rules that are restored with the Space and used by the runtime for retrieval and workflow routing

**Why Bundled Documents Matter:**
- Items provide quick facts (optimized for retrieval)
- Documents provide deep learning (full guides, examples, tutorials)
- Progressive disclosure: summary → detail as needed
- Fully self-contained: no external file dependencies

This is especially useful for teams that want policy, architecture, coding standards, or domain guidance to become part of the AI workflow automatically.

### Manual Space Selection (Coming Soon)

Disable auto-selection if you prefer manual control:
1. Click settings gear in chat
2. Toggle "Auto-select Spaces based on task" → OFF
3. Select Spaces manually before each query

### Citation Comments

Space attributions are added as comments:

```go
// Space Patterns Applied: [error-handling, table-tests] from [go-best-practices]
func TestUserAuth(t *testing.T) {
    // ...
}
```

**Toggle visibility:**
- Press `Ctrl+Shift+K` to show/hide citations (coming soon)
- Or disable in settings

### Session Override (Coming Soon)

Temporarily disable a Space:
1. Click "Needs Work" feedback
2. Select "Don't use this Space again this session"
3. Space skipped until you restart chat

---

## Quality Dashboard (Admin)

**Location:** http://127.0.0.1:9090/kb-quality (local daemon) or your deployment URL

### What You Can See

**Overview Stats:**
- Total Spaces tracked
- Average success rate across all Spaces
- Critical issues requiring attention
- Total uses (all-time)

**Top Performers:**
- Spaces with highest success rates
- Usage counts and task types
- Confidence scores

**Poor Performers:**
- Spaces with low success rates (<50%)
- Reasons: insufficient data, declining performance, or always failing
- Recommended actions: review, update, or disable

### Actions Available

**Run Quality Check Now:**
- Triggers immediate analysis
- Updates all metrics
- Identifies new issues

**Download Full Report:**
- Text format for offline review
- Includes all metrics and recommendations
- Suitable for team review meetings

**Refresh Data:**
- Reloads dashboard with latest data
- Updates after background jobs complete

---

## Troubleshooting

### "No Spaces available for this task"

**Cause:** No knowledge bases match your task type.

**Solutions:**
1. **Create relevant Spaces:** Add markdown files to your knowledge base folder
2. **Broaden task description:** Use keywords the AI recognizes
3. **Check Space status:** Visit quality dashboard to ensure Spaces are enabled

### "Space validation too strict"

**Cause:** Validation rules catching too many false positives.

**Solutions:**
1. **Give feedback:** Click "Needs Work" to lower Space priority
2. **Review Space content:** Update rules to be more flexible
3. **Use override:** Disable Space for current session (coming soon)

### "Feedback buttons not appearing"

**Cause:** Spaces weren't used for this generation.

**Check:**
1. Task type recognized? (implementation, debugging, testing, etc.)
2. Relevant Spaces exist? (visit quality dashboard)
3. Auto-selection enabled? (check settings)

### "Revision loop taking too long"

**Cause:** Complex validation issues requiring multiple fixes.

**Note:** System limits to 2 revision attempts automatically.

**If persistent:**
1. Give "Needs Work" feedback
2. Review Space rules for conflicts
3. Simplify validation rules temporarily

---

## Best Practices

### For Individual Users

**1. Give Regular Feedback**
- Aim for 50%+ of generations
- Be specific in optional comments
- Consistent feedback improves accuracy faster

**2. Review Explanations**
- Check "How I used expertise" after each generation
- Understand why Spaces were chosen
- Learn which Spaces work best for which tasks

**3. Monitor Quality Dashboard**
- Check weekly for trends
- Act on poor performers (update or disable)
- Celebrate top performers (share patterns with team)

**4. Curate Your Spaces**
- Keep knowledge bases up to date
- Remove outdated patterns
- Add new patterns as you learn

### For Teams

**1. Establish Space Ownership**
- Assign maintainers to each Space
- Regular review cycles (monthly)
- Document decision rationale in Space content

**2. Share Feedback Insights**
- Export quality reports for team meetings
- Discuss poor performers collaboratively
- Identify patterns in successful Spaces

**3. Create Domain-Specific Spaces**
- Backend: error handling, database patterns
- Frontend: component structure, state management
- Testing: test patterns, mocking strategies
- Security: auth patterns, input validation

**4. Version Control Your Spaces**
- Commit Space content to git
- Track changes over time
- Rollback if quality degrades

**5. Onboard New Members**
- Share this guide with new team members
- Explain your team's Spaces and their purposes
- Encourage feedback from fresh perspectives

---

## FAQ

**Q: Can I disable Spaces entirely?**
A: Yes. Simply remove all knowledge bases from your configuration, or disable auto-selection in settings.

**Q: Do Spaces send my code to the cloud?**
A: No. All Space operations run locally on your device. Only your configured LLM provider (Gemini, OpenAI) receives code when generating responses.

**Q: How many Spaces should I have?**
A: Start with 3-5 focused Spaces. Quality > quantity. Add more as you identify gaps.

**Q: Can I use someone else's Spaces?**
A: Yes! Import their knowledge base files into your Spaces folder. Give feedback to tune them for your needs.

**Q: What happens if two Spaces conflict?**
A: The system blends rules and applies the most specific guidance. If conflicts persist, give feedback to help prioritize.

**Q: How long until feedback improves results?**
A: Noticeable improvement after 10-15 feedback submissions per Space. System uses recency weighting (recent feedback counts more).

**Q: Can I see historical effectiveness data?**
A: Yes, via the Quality Dashboard at `/kb-quality`. Shows trends, success rates, and usage over time.

**Q: What if validation blocks legitimate code?**
A: Click "Needs Work" and add a comment explaining why. This trains the system. Future versions will support session overrides.

---

## Getting Help

**Documentation:**
- [Architecture Overview](ARCHITECTURE.md)
- [Space Implementation Guide](SPACE_EXPERT_IMPLEMENTATION_GUIDE.md)
- [KB Quality User Guide](KB_QUALITY_USER_GUIDE.md)
- [KB Custom Validators Guide](KB_CUSTOM_VALIDATORS_GUIDE.md)
- [KB Task Classification Guide](KB_TASK_CLASSIFICATION_GUIDE.md)
- [KB Quality Remaining Enhancements](KB_QUALITY_REMAINING_ENHANCEMENTS.md)

**Developer Resources:**
- `lib/sop/ai/agent/codingworkflow/` - Core implementation
- `lib/sop/ai/memory/kb_effectiveness.go` - Effectiveness tracking
- `lib/sop/tools/sopbackend/templates/kb_quality.html` - Dashboard UI

**Support:**
- Check Quality Dashboard for system health
- Review effectiveness metrics for patterns
- Export reports for detailed analysis

---

## What's Next

**Coming Soon:**
- Zero-config mode toggle (auto vs manual selection)
- Citation visibility keyboard shortcut
- Session-based Space overrides
- Expertise dashboard (per-session usage view)
- Refinement controls ("Apply Stricter", "Try Different Space")

**Planned:**
- Multi-Space blending (combine expertise from 2-3 Spaces)
- Custom validation rules (team-specific patterns)
- Space versioning and rollback
- Community Space marketplace

---

**Remember:** Spaces learn from your feedback. The more you use them and rate the results, the better they become at matching your style and needs.

**Start simple.** Give feedback on every few generations. Check the dashboard weekly. Your AI coding assistant will improve continuously.
