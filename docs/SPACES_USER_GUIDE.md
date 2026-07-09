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
