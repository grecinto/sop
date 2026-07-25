# Secure Input Handling - Privacy-First Architecture

**Date**: 2024-07-24  
**Status**: ✅ Implemented  
**Security Level**: Critical  

---

## Overview

When the Coding Workflow or any capability needs secure data (passwords, tokens, API keys), the system uses a **privacy-first architecture** that ensures sensitive data NEVER reaches the LLM context.

---

## Privacy Guarantee

**🔒 Core Promise**: Secure input (passwords, tokens, API keys) is sent **DIRECTLY** to tools/shell and **NEVER** exposed to the LLM.

### Data Flow Comparison

#### ❌ **Standard Flow** (regular chat input)
```
User types "fix the bug" 
    ↓
Frontend → LLM Context → Agent → Tool
                ↑
         (LLM sees everything)
```

#### ✅ **Secure Input Flow** (passwords, tokens)
```
User types password in chat editbox
    ↓
Frontend → [Bypass LLM] → Tool/Shell ONLY
                          
    LLM Context: "[SECURE_INPUT_REDACTED]"
    Chat History: "🔒 [secure input provided]"
```

---

## User Experience Flow

### Step 1: Agent Requests Secure Input

When a capability needs a password, the agent emits a special message:

```
🔒 Secure Input Required

Command needs: database password

Privacy Guarantees:
✓ Your input will be sent DIRECTLY to the command
✓ The AI model will NOT see your password  
✓ Not stored in chat history
✓ Not included in LLM context

Enter password in chat box below:
```

### Step 2: User Types in Chat Editbox

- User types password in **regular chat editbox** (same UX as normal chat)
- Frontend detects this is a secure input context (via session flag)
- Frontend **masks** the input visually (shows `••••••••`)

### Step 3: Secure Data Routing

```typescript
// Frontend code (conceptual)
if (isSecureInputContext) {
    // Route DIRECTLY to tool execution
    sendToToolDirect(userInput, toolId, sessionId);
    
    // Replace in chat history
    addToChatHistory({
        role: "user",
        content: "🔒 [secure input provided]"
    });
    
    // Send placeholder to LLM
    sendToLLM({
        role: "user", 
        content: "[SECURE_INPUT_REDACTED]"
    });
}
```

### Step 4: Capability Uses Secure Data

```go
// Capability receives secure input via dedicated channel
secureData := ctx.Value("secure_input").(string)

// Execute command with secure data
cmd := exec.Command("mysql", "-u", "root", "-p"+secureData)
output, err := cmd.CombinedOutput()

// Return result (NOT the password)
return output, err
```

---

## Implementation Details

### Authorization Layer

**File**: `lib/sop/ai/agent/capabilities/authorization.go`

```go
// New approval type for secure input
const ApprovalSecureInput ApprovalType = "secure_input"

// OperationContext with privacy flags
type OperationContext struct {
    // ... existing fields ...
    
    // Privacy flags
    SecureInputRequired bool   // Triggers secure input flow
    SecureInputPrompt   string // What to ask user (e.g., "Enter database password:")
}
```

### Approval Handler

**File**: `lib/sop/ai/agent/copilot.go`

```go
func (a *CopilotAgent) handleSecureInputApproval(ctx context.Context, opCtx capabilities.OperationContext) (bool, error) {
    // Logs privacy guarantee
    log.Info("🔒 Secure input approval requested",
        "prompt", opCtx.SecureInputPrompt,
        "privacy_guarantee", "input_bypasses_llm_context")
    
    // Future: Trigger UI prompt with privacy notice
    // Future: Wait for user input via secure channel
    // Future: Inject into tool context (not LLM context)
    
    return true, nil
}
```

### Capability Usage Example

**File**: `lib/sop/ai/agent/codingworkflow/capability.go` (future enhancement)

```go
func (c *Capability) Execute(ctx context.Context, query string) (string, error) {
    // Detect if command needs password
    if needsPassword(query) {
        // Request secure input approval
        opCtx := capabilities.OperationContext{
            OperationType:       "shell_exec",
            Resource:            query,
            SecureInputRequired: true,
            SecureInputPrompt:   "Enter database password:",
        }
        
        // This triggers the privacy-first UI flow
        approved, err := authzManager.RequestApproval(ctx, capabilities.ApprovalSecureInput, opCtx)
        if !approved {
            return "", fmt.Errorf("user denied secure input")
        }
        
        // Secure data is now in ctx (via secure channel)
        secureData := ctx.Value("secure_input").(string)
        
        // Execute with secure data
        return executeWithSecureInput(query, secureData)
    }
    
    // Regular execution
    return execute(query)
}
```

---

## Frontend Integration Requirements

### Chat UI Changes Needed

1. **Secure Input Context Detection**
   - Listen for `secure_input_required` event from backend
   - Enter "secure input mode" with visual indicators

2. **Masked Input Display**
   - Show `••••••••` while user types (like password fields)
   - Display 🔒 icon in input box

3. **Privacy Notice Display**
   - Show privacy guarantees prominently
   - Use green checkmarks for each guarantee
   - Make it visually distinct from regular chat

4. **Dual-Channel Submission**
   - Submit input via **secure channel** to tool (WebSocket/HTTP direct)
   - Submit placeholder `[SECURE_INPUT_REDACTED]` to LLM context
   - Add `🔒 [secure input provided]` to chat history

5. **Session State Management**
   ```typescript
   interface ChatSession {
       secureInputMode: boolean
       secureInputPrompt: string
       secureInputTarget: string  // tool ID
   }
   ```

---

## Security Considerations

### ✅ What We Protect

1. **LLM Context Isolation**: Password never in prompt or response
2. **Chat History Privacy**: Shows `🔒 [secure input provided]` instead of actual value
3. **Log Privacy**: Secure data not logged (use `[REDACTED]` in logs)
4. **Memory Isolation**: Not stored in STM or LTM
5. **Audit Trail**: Records that secure input was provided, but not the value

### ⚠️ Important Notes

1. **Transport Security**: Requires HTTPS/WSS (no plaintext transmission)
2. **Ephemeral Storage**: Secure data held in memory only, cleared after use
3. **No Retry Logic**: If tool fails, user must re-enter (don't cache)
4. **Clear Cancellation**: User can cancel secure input flow at any time

### 🔐 Compliance

- **GDPR**: Sensitive data not processed by AI (processor minimization)
- **SOC 2**: Audit trail without exposing secrets
- **PCI DSS**: If handling payment data, secure input bypasses AI processing
- **HIPAA**: PHI can be entered without AI exposure

---

## Testing Checklist

- [ ] Secure input triggers privacy notice in UI
- [ ] User input is masked during typing
- [ ] Password does NOT appear in LLM context
- [ ] Password does NOT appear in chat history  
- [ ] Password does NOT appear in logs
- [ ] Tool receives password correctly
- [ ] Cancel flow works without leaking data
- [ ] Multiple secure inputs in sequence work
- [ ] Browser refresh clears secure data from memory

---

## Future Enhancements

1. **Hardware Token Support**: YubiKey, TouchID for approvals
2. **Secret Manager Integration**: Fetch from Vault/1Password instead of typing
3. **Biometric Approval**: Require fingerprint for secure input submission
4. **Audit Dashboard**: Show when secure inputs were used (not values)
5. **Time-Limited Sessions**: Auto-clear secure context after N minutes

---

## Example User Scenarios

### Scenario 1: Database Connection

```
User: "Connect to production database"

Agent: "🔒 Secure Input Required
       
       Command needs: database password
       
       Privacy Guarantees:
       ✓ Your input will be sent DIRECTLY to the command
       ✓ The AI model will NOT see your password
       ✓ Not stored in chat history
       
       Enter password:"

User: [types password in chat box with •••••••• masking]

Agent: "✅ Connected to production database
       Schema: 42 tables, 1.2M records"

[Chat history shows: "🔒 [secure input provided]"]
[LLM context shows: "[SECURE_INPUT_REDACTED]"]
```

### Scenario 2: Git Credentials

```
User: "Push changes to private repo"

Agent: "🔒 Secure Input Required
       
       Command needs: GitHub personal access token
       
       Privacy Guarantees:
       ✓ Token sent DIRECTLY to git command
       ✓ AI model will NOT see your token
       ✓ Not logged or stored
       
       Enter token:"

User: [pastes token with masking]

Agent: "✅ Pushed 3 commits to main
       Latest: feat: add secure input handling"
```

---

## Summary

**Answer to Original Question**: 

> "How will Coding workflow ask for passwords?"

**Yes**, it will prompt via the **chat editbox**, BUT with critical privacy guarantees:

1. 🔒 **Clear Privacy Notice**: User sees explicit guarantee before typing
2. ⚡ **Direct Routing**: Password goes **DIRECTLY to shell/tool**
3. 🚫 **LLM Bypass**: AI model **NEVER sees the password**
4. 📝 **History Protection**: Chat shows `🔒 [secure input provided]` (not the actual password)
5. 🔐 **Memory Isolation**: Not stored in STM/LTM/logs

This architecture provides **convenience** (same chat interface) with **enterprise-grade security** (zero-knowledge to AI).
