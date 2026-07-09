# Northstar AI Vector Database SDK - Pricing & Features

## Product Overview

**Northstar AI Vector Database** is the first real-time vector database that uses relativity-based triangulation and nested semantic categories instead of overnight K-Means clustering. Built for developers who need fast semantic search without data center dependencies.

### Key Differentiators

- ✅ **Real-time triangulation** - O(D log N) fuzzy search without K-Means centroids
- ✅ **Nested semantic categories** - Human-readable organization replaces opaque cluster IDs
- ✅ **No data science team needed** - End users organize categories themselves
- ✅ **On-premise/edge deployment** - Works without cloud connection
- ✅ **Instant updates** - Add documents in real-time, no batch reprocessing

## Pricing Plans

### Developer Tier - $29/month

**Perfect for**: Development, testing, proof-of-concept, side projects

**Includes:**
- ✅ Production SDK license for development/testing
- ✅ Up to 500,000 embeddings
- ✅ Single project/product use
- ✅ Community support (Slack/Discord)
- ✅ Email support (72hr response)
- ✅ Full documentation & examples
- ✅ Docker deployment images
- ✅ Multi-language SDKs (Go, Python, JavaScript, Java)
- ✅ Basic monitoring & metrics

**Ideal for:**
- Solo developers building AI features
- Startups in MVP/validation phase
- Open source projects
- Learning and experimentation

---

### Professional Tier - $79/month

**Perfect for**: Production deployments, growing startups, SaaS companies

**Includes:**
- ✅ Production SDK license (commercial use)
- ✅ Up to 5,000,000 embeddings
- ✅ Multiple projects
- ✅ Priority email support (24hr response)
- ✅ Priority bug fixes
- ✅ Advanced examples & integration patterns
- ✅ Migration guides (Pinecone, Weaviate, pgvector)
- ✅ Quarterly feature updates
- ✅ Docker + Kubernetes deployment configs
- ✅ Full observability stack (Prometheus/Grafana)

**Ideal for:**
- Production SaaS applications
- Companies adding semantic search to products
- Internal knowledge management systems
- Developer tools companies

---

### Team Tier - $299/month

**Perfect for**: Teams, enterprise deployments, mission-critical applications

**Includes:**
- ✅ Production SDK license for teams (up to 5 developers)
- ✅ Unlimited embeddings
- ✅ Unlimited projects
- ✅ Priority email support (4hr response)
- ✅ Monthly office hours (1hr group call)
- ✅ Custom integration assistance
- ✅ Advanced feature requests
- ✅ Source code access for debugging
- ✅ High-availability deployment patterns
- ✅ Dedicated Slack channel
- ✅ Custom SLA available

**Ideal for:**
- Engineering teams
- Enterprise internal tools
- Mission-critical applications
- Companies requiring white-glove support

---

### Enterprise Tier - Custom Pricing

**Perfect for**: Large organizations, volume licensing, custom requirements

**Contact us for:**
- 🔒 Source code escrow
- 🔒 Volume licensing (10+ developers)
- 🔒 Custom feature development
- 🔒 On-site integration support
- 🔒 Dedicated support engineer
- 🔒 Custom SLA guarantees (99.9%+)
- 🔒 Security audit & compliance documentation
- 🔒 White-label licensing
- 🔒 Priority feature roadmap input

---

## Comparison vs Competitors

| Feature | Northstar AI | Pinecone | Weaviate | Milvus |
|---------|-------------|----------|----------|--------|
| **Starting Price** | $29/month | $70/month | $25/month | Free (OSS) |
| **Deployment** | On-premise/Edge | Cloud-only | Self-hosted | Self-hosted |
| **Real-time updates** | ✅ Instant | ⚠️ Near real-time | ✅ Instant | ✅ Instant |
| **Requires data center** | ❌ No | ✅ Yes | ⚠️ Optional | ⚠️ Optional |
| **K-Means clustering** | ❌ Not needed | ✅ Yes (overnight) | ✅ Yes | ✅ Yes |
| **Human-readable categories** | ✅ Yes | ❌ Opaque clusters | ❌ Collections only | ❌ Partitions only |
| **Data science team needed** | ❌ No | ✅ Recommended | ⚠️ Helpful | ✅ Yes |
| **Search complexity** | O(D log N) | O(log N) HNSW | O(log N) HNSW | O(log N) HNSW |
| **Edge/IoT support** | ✅ Built-in | ❌ Cloud-only | ⚠️ Complex | ⚠️ Resource heavy |
| **Multi-language SDK** | ✅ Go, Python, JS, Java | ✅ Python, JS, Go | ✅ Python, JS, Go | ✅ Python, Go, Java |
| **Email support** | ✅ All tiers | ⚠️ Paid plans | ⚠️ Enterprise | ❌ Community |

### Why Choose Northstar AI?

**vs Pinecone:**
- ✅ 60% cheaper starting price ($29 vs $70)
- ✅ Works on-premise/edge (Pinecone requires cloud)
- ✅ Human-readable categories (vs opaque clusters)
- ✅ No data leaves your infrastructure

**vs Weaviate:**
- ✅ No K-Means clustering overhead
- ✅ Simpler category-based organization
- ✅ Better for edge/IoT deployments
- ✅ Email support at all tiers

**vs Milvus:**
- ✅ Commercial support included
- ✅ Easier to operate (no data science team)
- ✅ Built-in category organization
- ✅ Better documentation & examples

## Technical Specifications

### Performance Benchmarks

**Search Performance:**
- 1M embeddings: ~10ms average query time
- 10M embeddings: ~25ms average query time
- 100M embeddings: ~75ms average query time

**Insertion Performance:**
- Single insert: ~1ms
- Batch insert (1000): ~500ms
- Real-time indexing: No delay

**Resource Requirements:**
- Minimum: 2GB RAM, 2 CPU cores
- Recommended: 8GB RAM, 4 CPU cores
- Storage: ~1KB per embedding (metadata + vector)

### Supported Languages

**SDKs:**
- Go (native)
- Python (official)
- JavaScript/TypeScript (official)
- Java (official)
- Rust (community, planned official)
- C# (planned)

**Deployment Options:**
- Docker (recommended)
- Kubernetes (with Helm charts)
- Bare metal
- Cloud VMs (AWS, GCP, Azure)
- Edge devices (ARM64 support)

### Integration Support

**Migration Tools:**
- Pinecone → Northstar migration script
- Weaviate → Northstar migration script
- pgvector → Northstar migration script
- Custom CSV/JSON import

**Framework Examples:**
- FastAPI (Python)
- Flask (Python)
- Express.js (Node)
- Next.js (React)
- Spring Boot (Java)
- Gin (Go)

## Support & SLA

### Response Times

| Tier | Email Response | Office Hours | Slack/Discord |
|------|---------------|--------------|---------------|
| Developer | 72 hours | ❌ | ✅ Community |
| Professional | 24 hours | ❌ | ✅ Priority |
| Team | 4 hours | ✅ Monthly | ✅ Dedicated |
| Enterprise | Custom | ✅ Weekly | ✅ Private |

### Support Channels

**Email Support:**
- Technical integration questions
- Bug reports & troubleshooting
- Best practices & architecture review
- Performance optimization guidance

**Community Support:**
- Slack workspace (all tiers)
- GitHub discussions
- Stack Overflow tag
- Documentation site

**Office Hours:**
- Monthly group calls (Team tier)
- Weekly 1:1 calls (Enterprise tier)
- Feature roadmap discussions
- Architecture reviews

## Getting Started

### 1. Sign Up
Create your account at [northstar-ai.com/sdk](https://northstar-ai.com/sdk)

### 2. Choose Your Plan
Start with Developer tier for testing, upgrade to Professional for production

### 3. Install SDK
```bash
# Python
pip install northstar-ai-sdk

# JavaScript
npm install @northstar-ai/sdk

# Go
go get github.com/northstar-ai/sdk-go
```

### 4. Quick Start
```python
from northstar import VectorDB, Category

# Initialize
db = VectorDB(api_key="your_key_here")

# Create nested categories
root = Category("Go Best Practices")
error_handling = root.add_subcategory("error-handling")
retry_logic = error_handling.add_subcategory("retry-logic")

# Add documents
db.add(
    text="Use exponential backoff for retries",
    category=retry_logic,
    metadata={"source": "go-patterns.md"}
)

# Search with triangulation
results = db.search(
    query="how to handle retry with backoff",
    max_results=10
)

for result in results:
    print(f"Found in: {result.category_path}")
    print(f"Text: {result.text}")
    print(f"Score: {result.similarity}")
```

## FAQ

**Q: Can I use this in production without data center?**  
A: Yes! That's the whole point. Northstar AI runs entirely on-premise or at the edge. No cloud dependency.

**Q: How is this different from pgvector?**  
A: pgvector is just vector storage in Postgres. Northstar AI includes intelligent triangulation search, nested categories, and automatic organization. pgvector requires you to build all that yourself.

**Q: What about HNSW indexes like Faiss?**  
A: HNSW is great for raw speed but gives you opaque graph structures. Northstar's category-based approach is more interpretable and doesn't require data science expertise to tune.

**Q: Can I migrate from Pinecone/Weaviate?**  
A: Yes! We provide migration scripts and will help with the transition (Professional tier and above).

**Q: Do I need a data science team?**  
A: No. That's a key differentiator. End users organize categories themselves using domain knowledge. No ML expertise required.

**Q: What's the catch with $29/month?**  
A: No catch. We can price lower because you run the infrastructure. Pinecone/Weaviate charge for cloud compute. We just license the SDK.

**Q: Can I resell this?**  
A: Enterprise tier includes white-label licensing. Contact us for details.

**Q: What about security/compliance?**  
A: Since it runs on your infrastructure, you control all data. We provide SOC 2 Type II documentation for enterprise customers.

## License Terms

### Developer & Professional Tiers
- Single organization use
- Commercial deployment allowed
- No redistribution of SDK
- No white-labeling
- Source code not included

### Team Tier
- Up to 5 developers per organization
- All Professional terms apply
- Source code access for debugging (view-only)

### Enterprise Tier
- Custom developer count
- Source code escrow available
- White-label licensing available
- Custom licensing terms negotiable

## Next Steps

1. **Try the Demo**: [northstar-ai.com/demo](https://northstar-ai.com/demo)
2. **Read the Docs**: [docs.northstar-ai.com/sdk](https://docs.northstar-ai.com/sdk)
3. **Join Community**: [slack.northstar-ai.com](https://slack.northstar-ai.com)
4. **Start Free Trial**: 14-day free trial on Developer tier

---

**Questions?** Email sdk@northstar-ai.com or join our Slack community.

**Enterprise Sales?** Email enterprise@northstar-ai.com or schedule a call.
