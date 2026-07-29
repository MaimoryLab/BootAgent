import { useState } from "react";
import {
  ArrowRight,
  ArrowUpRight,
  Bot,
  Check,
  ChevronDown,
  Command,
  GitBranch,
  Layers3,
  Menu,
  Network,
  ShieldCheck,
  Sparkles,
  TerminalSquare,
  X,
  Zap,
} from "lucide-react";
import { Link } from "react-router-dom";

type DemoTab = "overview" | "routing" | "profiles";

const demoTabs: Array<{ id: DemoTab; label: string }> = [
  { id: "overview", label: "Overview" },
  { id: "routing", label: "Routing" },
  { id: "profiles", label: "Profiles" },
];

const agents = [
  { name: "Codex", detail: "Responses API", tone: "coral" },
  { name: "Claude Code", detail: "Anthropic", tone: "mint" },
  { name: "OpenCode", detail: "OpenAI compatible", tone: "blue" },
  { name: "Aider", detail: "Terminal workflow", tone: "gold" },
];

const faqs = [
  {
    question: "Does OneAgent replace my coding agents?",
    answer:
      "No. OneAgent sits beside them as a small control plane. You keep the tools you already like and use OneAgent to manage the provider, model, and profile each one should use.",
  },
  {
    question: "Where does my configuration live?",
    answer:
      "Locally by default. OneAgent keeps the working configuration on your machine and gives you an explicit view of what each agent is connected to before you apply a change.",
  },
  {
    question: "Can different agents use different models?",
    answer:
      "Yes. Each agent gets its own lane, so a terminal agent can use one provider while an IDE extension or review agent uses another. Switch the binding without editing scattered dotfiles.",
  },
];

function scrollToSection(id: string) {
  document.getElementById(id)?.scrollIntoView({ behavior: "smooth", block: "start" });
}

function BrandMark() {
  return (
    <span className="landing-brand-mark" aria-hidden="true">
      <span className="landing-brand-node landing-brand-node-left" />
      <span className="landing-brand-node landing-brand-node-right" />
      <span className="landing-brand-node landing-brand-node-bottom" />
      <span className="landing-brand-link landing-brand-link-one" />
      <span className="landing-brand-link landing-brand-link-two" />
    </span>
  );
}

function NetworkMap() {
  return (
    <div className="network-map" aria-label="OneAgent routing map preview" role="img">
      <div className="network-map-grid" />
      <svg className="network-map-lines" viewBox="0 0 640 430" aria-hidden="true">
        <path d="M320 212 C256 184 182 144 98 93" />
        <path d="M320 212 C386 182 464 145 548 94" />
        <path d="M320 212 C248 258 178 294 102 344" />
        <path d="M320 212 C393 256 465 298 548 344" />
        <circle cx="98" cy="93" r="4" />
        <circle cx="548" cy="94" r="4" />
        <circle cx="102" cy="344" r="4" />
        <circle cx="548" cy="344" r="4" />
      </svg>

      <div className="network-map-radar network-map-radar-one" />
      <div className="network-map-radar network-map-radar-two" />

      <div className="network-core">
        <div className="network-core-orbit" />
        <div className="network-core-icon"><Network size={22} strokeWidth={1.8} /></div>
        <strong>OneAgent</strong>
        <span>control plane</span>
      </div>

      {agents.map((agent, index) => (
        <div className={`network-agent network-agent-${index + 1}`} key={agent.name}>
          <span className={`network-agent-dot network-agent-dot-${agent.tone}`} />
          <span>
            <strong>{agent.name}</strong>
            <small>{agent.detail}</small>
          </span>
          <span className="network-agent-state">ready</span>
        </div>
      ))}

      <div className="network-map-caption">
        <span><span className="network-live-dot" /> local workspace</span>
        <span>profile / default</span>
      </div>
    </div>
  );
}

function DemoOverview() {
  return (
    <div className="demo-panel-content demo-overview">
      <div className="demo-status-row">
        <div>
          <span className="demo-overline">WORKSPACE</span>
          <strong>Personal · default</strong>
        </div>
        <span className="demo-ready"><span /> All systems ready</span>
      </div>
      <div className="demo-agent-list">
        {agents.slice(0, 3).map((agent, index) => (
          <div className="demo-agent-row" key={agent.name}>
            <span className={`demo-agent-avatar demo-agent-avatar-${agent.tone}`}>
              {index === 0 ? <TerminalSquare size={14} /> : index === 1 ? <Bot size={14} /> : <Command size={14} />}
            </span>
            <span className="demo-agent-name"><strong>{agent.name}</strong><small>{agent.detail}</small></span>
            <span className="demo-agent-model">{index === 0 ? "gpt-5-codex" : index === 1 ? "claude-sonnet" : "deepseek-v3"}</span>
            <Check size={15} className="demo-check" />
          </div>
        ))}
      </div>
      <div className="demo-footer-row">
        <span><span className="demo-mini-dot" /> Changes are local</span>
        <span>Updated just now</span>
      </div>
    </div>
  );
}

function DemoRouting() {
  return (
    <div className="demo-panel-content demo-routing">
      <div className="demo-routing-header">
        <div>
          <span className="demo-overline">REQUEST PATH</span>
          <strong>agent → profile → provider</strong>
        </div>
        <span className="demo-route-badge">3 hops</span>
      </div>
      <div className="route-track">
        <div className="route-node route-node-coral"><TerminalSquare size={16} /><span>Codex</span><small>agent</small></div>
        <span className="route-line"><i /></span>
        <div className="route-node route-node-mint"><Layers3 size={16} /><span>default</span><small>profile</small></div>
        <span className="route-line"><i /></span>
        <div className="route-node route-node-blue"><Zap size={16} /><span>PPIO</span><small>provider</small></div>
      </div>
      <div className="route-code">
        <span className="code-muted">$</span> oneagent agent set codex
        <br />
        <span className="code-muted">→</span> profile <b>default</b> · model <b>gpt-5-codex</b>
      </div>
      <div className="demo-footer-row"><span><span className="demo-mini-dot demo-mini-dot-coral" /> Applied without touching your prompt</span></div>
    </div>
  );
}

function DemoProfiles() {
  return (
    <div className="demo-panel-content demo-profiles">
      <div className="demo-status-row">
        <div>
          <span className="demo-overline">SAVED PROFILES</span>
          <strong>Switch context, not config files</strong>
        </div>
        <span className="demo-add-profile">＋ New profile</span>
      </div>
      <div className="profile-preview-list">
        <div className="profile-preview profile-preview-active">
          <div className="profile-preview-top"><span className="profile-symbol profile-symbol-coral">⌁</span><strong>default</strong><span className="profile-current">current</span></div>
          <p>Balanced for everyday building and review.</p>
          <div className="profile-preview-meta"><span>3 agents</span><span>2 providers</span></div>
        </div>
        <div className="profile-preview">
          <div className="profile-preview-top"><span className="profile-symbol profile-symbol-mint">✦</span><strong>deep-work</strong></div>
          <p>Higher reasoning budget for long-running tasks.</p>
          <div className="profile-preview-meta"><span>1 agent</span><span>1 provider</span></div>
        </div>
      </div>
    </div>
  );
}

function DemoPanel({ activeTab }: { activeTab: DemoTab }) {
  if (activeTab === "routing") return <DemoRouting />;
  if (activeTab === "profiles") return <DemoProfiles />;
  return <DemoOverview />;
}

function AppWordmark() {
  return (
    <Link className="landing-wordmark" to="/" aria-label="OneAgent home">
      <BrandMark />
      <span>OneAgent</span>
    </Link>
  );
}

export function LandingPage() {
  const [activeDemo, setActiveDemo] = useState<DemoTab>("overview");
  const [mobileOpen, setMobileOpen] = useState(false);
  const [openFaq, setOpenFaq] = useState<number | null>(0);

  const goTo = (id: string) => {
    setMobileOpen(false);
    scrollToSection(id);
  };

  return (
    <div className="landing-shell">
      <div className="landing-background-grid" aria-hidden="true" />
      <header className="landing-header">
        <nav className="landing-nav" aria-label="Primary navigation">
          <AppWordmark />
          <div className={`landing-nav-links${mobileOpen ? " is-open" : ""}`}>
            <button type="button" onClick={() => goTo("product")}>Product</button>
            <button type="button" onClick={() => goTo("workflow")}>Workflow</button>
            <button type="button" onClick={() => goTo("teams")}>For teams</button>
            <button type="button" onClick={() => goTo("faq")}>FAQ</button>
            <Link className="landing-nav-cta landing-nav-cta-mobile" to="/setup/agents">Open workspace <ArrowUpRight size={15} /></Link>
          </div>
          <div className="landing-nav-actions">
            <Link className="landing-nav-login" to="/setup/agents">Sign in</Link>
            <Link className="landing-nav-cta" to="/setup/agents">Open workspace <ArrowUpRight size={15} /></Link>
            <button
              className="landing-mobile-toggle"
              type="button"
              aria-label={mobileOpen ? "Close menu" : "Open menu"}
              aria-expanded={mobileOpen}
              onClick={() => setMobileOpen((value) => !value)}
            >
              {mobileOpen ? <X size={21} /> : <Menu size={21} />}
            </button>
          </div>
        </nav>
      </header>

      <main>
        <section className="landing-hero" aria-labelledby="hero-heading">
          <div className="hero-copy">
            <h1 id="hero-heading">All your agents.<br /><span>One clear lane.</span></h1>
            <p className="hero-lede">
              OneAgent is the calm control plane for coding agents. Keep providers, models, and profiles in one place — then get back to the work that made you open the terminal.
            </p>
            <div className="hero-actions">
              <Link className="landing-button landing-button-primary" to="/setup/agents">
                Start with OneAgent <ArrowRight size={17} />
              </Link>
              <button className="landing-button landing-button-quiet" type="button" onClick={() => goTo("workflow")}>
                See the workflow <span className="button-arrow">↘</span>
              </button>
            </div>
            <div className="hero-note"><span className="hero-note-dot" /> Local-first by default <span className="hero-note-divider" /> No scattered dotfiles</div>
          </div>
          <div className="hero-visual-wrap">
            <div className="hero-visual-label"><span>LIVE ROUTING MAP</span><span><span className="hero-live-dot" /> synced</span></div>
            <NetworkMap />
            <div className="hero-visual-footnote"><span>OneAgent workspace</span><span>macOS · local</span></div>
          </div>
        </section>

        <section className="tool-strip" aria-label="Supported agent tools">
          <span className="tool-strip-label">Works with the tools already in your terminal</span>
          <div className="tool-strip-list">
            <span className="tool-name"><TerminalSquare size={16} /> Codex</span>
            <span className="tool-name"><Bot size={16} /> Claude Code</span>
            <span className="tool-name"><Command size={16} /> OpenCode</span>
            <span className="tool-name"><GitBranch size={16} /> Aider</span>
            <span className="tool-name"><Sparkles size={16} /> Kilo CLI</span>
          </div>
        </section>

        <section className="landing-section landing-section-paper" id="product" aria-labelledby="product-heading">
          <div className="landing-section-inner">
            <div className="section-heading-row">
              <span className="section-index">01</span>
              <div className="section-heading-copy">
                <h2 id="product-heading">The messy part is the handoff.</h2>
                <p>Provider changes should not mean hunting through five config files, remembering which agent uses which model, and hoping the next restart picked it up.</p>
              </div>
            </div>
            <div className="friction-layout">
              <div className="friction-statement">
                <span className="friction-quote-mark">“</span>
                <p>Move from <em>where is this configured?</em> to <strong>ship the change</strong>.</p>
                <span className="friction-rule" />
                <span className="friction-caption">One place to see the decision. One action to apply it.</span>
              </div>
              <div className="friction-list">
                <div className="friction-item">
                  <span className="friction-icon friction-icon-coral"><Command size={18} /></span>
                  <div><strong>Choose once</strong><span>Bind each agent to the provider and model that fit its job.</span></div>
                  <span className="friction-number">01</span>
                </div>
                <div className="friction-item">
                  <span className="friction-icon friction-icon-mint"><Layers3 size={18} /></span>
                  <div><strong>Save the shape</strong><span>Keep a named profile for a project, a client, or a different mode of thinking.</span></div>
                  <span className="friction-number">02</span>
                </div>
                <div className="friction-item">
                  <span className="friction-icon friction-icon-blue"><Zap size={18} /></span>
                  <div><strong>Apply with confidence</strong><span>See the resulting route before you restart anything.</span></div>
                  <span className="friction-number">03</span>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section className="landing-section landing-section-ink" id="workflow" aria-labelledby="workflow-heading">
          <div className="landing-section-inner workflow-section-inner">
            <div className="section-heading-row section-heading-row-dark">
              <span className="section-index">02</span>
              <div className="section-heading-copy">
                <h2 id="workflow-heading">From provider swap to done in three moves.</h2>
                <p>OneAgent keeps the mental model small: select the lane, preview the route, apply the change.</p>
              </div>
            </div>

            <div className="workflow-layout">
              <div className="workflow-steps">
                <div className="workflow-step workflow-step-active">
                  <span className="workflow-step-number">01</span>
                  <div><strong>Pick the agents</strong><p>Start with the tools you actually use. Nothing else gets touched.</p></div>
                  <Check size={18} />
                </div>
                <div className="workflow-step">
                  <span className="workflow-step-number">02</span>
                  <div><strong>Choose the route</strong><p>Pair a profile with a provider and model — per agent, not per machine.</p></div>
                  <ArrowRight size={18} />
                </div>
                <div className="workflow-step">
                  <span className="workflow-step-number">03</span>
                  <div><strong>Keep moving</strong><p>Apply once, restart when you are ready, and return to a legible workspace.</p></div>
                  <ArrowRight size={18} />
                </div>
                <Link className="workflow-link" to="/setup/agents">Open the real setup flow <ArrowUpRight size={16} /></Link>
              </div>

              <div className="demo-window" id="demo-preview">
                <div className="demo-window-bar">
                  <div className="demo-window-lights"><span /><span /><span /></div>
                  <span>oneagent / workspace</span>
                  <span className="demo-window-privacy"><ShieldCheck size={13} /> local</span>
                </div>
                <div className="demo-tabs" role="tablist" aria-label="OneAgent preview">
                  {demoTabs.map((tab) => (
                    <button
                      key={tab.id}
                      className={`demo-tab${activeDemo === tab.id ? " is-active" : ""}`}
                      type="button"
                      role="tab"
                      aria-selected={activeDemo === tab.id}
                      onClick={() => setActiveDemo(tab.id)}
                    >
                      {tab.label}
                    </button>
                  ))}
                </div>
                <DemoPanel activeTab={activeDemo} />
              </div>
            </div>
          </div>
        </section>

        <section className="landing-section landing-section-paper landing-section-lanes" id="teams" aria-labelledby="teams-heading">
          <div className="landing-section-inner">
            <div className="section-heading-row">
              <span className="section-index">03</span>
              <div className="section-heading-copy">
                <h2 id="teams-heading">Every agent gets its own lane.</h2>
                <p>Keep the team’s tooling flexible without turning the workspace into a shared mystery.</p>
              </div>
            </div>
            <div className="lanes-layout">
              <div className="lanes-copy">
                <div className="lanes-copy-head"><span className="lanes-kicker">A SMALLER SURFACE AREA</span><Network size={22} /></div>
                <p>OneAgent makes the invisible parts of an AI development stack inspectable: who is connected, where requests go, and which profile is active.</p>
                <ul className="check-list">
                  <li><Check size={16} /> Per-agent provider bindings</li>
                  <li><Check size={16} /> Named profiles for repeatable context</li>
                  <li><Check size={16} /> A readable preview before activation</li>
                </ul>
                <Link className="text-link" to="/setup/agents">Explore the workspace <ArrowUpRight size={16} /></Link>
              </div>
              <div className="lane-stack" aria-label="Agent lanes preview">
                {agents.map((agent, index) => (
                  <div className={`lane-row lane-row-${agent.tone}`} key={agent.name}>
                    <span className="lane-order">0{index + 1}</span>
                    <span className="lane-color-bar" />
                    <span className="lane-agent"><strong>{agent.name}</strong><small>{agent.detail}</small></span>
                    <span className="lane-route"><span>profile / {index === 1 ? "deep-work" : "default"}</span><ArrowRight size={14} /><span>{index === 2 ? "Novita" : "PPIO"}</span></span>
                    <span className="lane-state"><span /> ready</span>
                  </div>
                ))}
                <div className="lane-stack-foot"><span>4 lanes</span><span>1 workspace</span><span>0 guesswork</span></div>
              </div>
            </div>
          </div>
        </section>

        <section className="principles-section" aria-labelledby="principles-heading">
          <div className="principles-inner">
            <div className="principles-orbit" aria-hidden="true">
              <span className="principles-orbit-ring principles-orbit-ring-one" />
              <span className="principles-orbit-ring principles-orbit-ring-two" />
              <span className="principles-orbit-core"><ShieldCheck size={26} /></span>
            </div>
            <div className="principles-copy">
              <span className="section-index section-index-accent">04</span>
              <h2 id="principles-heading">The best infrastructure disappears into clarity.</h2>
              <p>OneAgent is intentionally small, local, and explicit. It gives your team a shared vocabulary for model routing without asking your workflow to become a new platform.</p>
              <div className="principles-notes">
                <span><span className="principle-dot principle-dot-mint" /> Local by default</span>
                <span><span className="principle-dot principle-dot-coral" /> Explicit before apply</span>
                <span><span className="principle-dot principle-dot-gold" /> Easy to return to</span>
              </div>
            </div>
          </div>
        </section>

        <section className="faq-section" id="faq" aria-labelledby="faq-heading">
          <div className="landing-section-inner faq-inner">
            <div className="faq-intro">
              <span className="section-index">05</span>
              <h2 id="faq-heading">A few good questions.</h2>
              <p>Short answers for the moment before you open the workspace.</p>
            </div>
            <div className="faq-list">
              {faqs.map((faq, index) => {
                const isOpen = openFaq === index;
                return (
                  <div className={`faq-item${isOpen ? " is-open" : ""}`} key={faq.question}>
                    <button type="button" aria-expanded={isOpen} onClick={() => setOpenFaq(isOpen ? null : index)}>
                      <span>{faq.question}</span>
                      <ChevronDown size={18} />
                    </button>
                    <div className="faq-answer" hidden={!isOpen}><p>{faq.answer}</p></div>
                  </div>
                );
              })}
            </div>
          </div>
        </section>

        <section className="final-cta" aria-labelledby="final-cta-heading">
          <div className="final-cta-grid" aria-hidden="true" />
          <div className="final-cta-inner">
            <div className="final-cta-mark"><BrandMark /></div>
            <h2 id="final-cta-heading">Make the next model swap boring.</h2>
            <p>Set up your agents once. Keep the useful part of your attention for the work itself.</p>
            <Link className="landing-button landing-button-primary landing-button-final" to="/setup/agents">Open OneAgent <ArrowRight size={17} /></Link>
            <span className="final-cta-note">No account wall. No new editor. Just a clearer route.</span>
          </div>
        </section>
      </main>

      <footer className="landing-footer">
        <div className="landing-footer-inner">
          <AppWordmark />
          <span className="landing-footer-copy">One control plane for the tools that build with you.</span>
          <div className="landing-footer-links"><Link to="/setup/agents">Workspace</Link><button type="button" onClick={() => scrollToSection("faq")}>FAQ</button><span>© 2026 OneAgent</span></div>
        </div>
      </footer>
    </div>
  );
}
