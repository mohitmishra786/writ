# The mentor briefing and the published request mechanic

Contents:

- What every mentor briefing contains regardless of depth.
- The help-versus-build boundary written out.
- A positive and negative worked pair.
- Power-dynamics guidance.
- The sponsor-representative engagement rule.
- The request mechanic as participants read it.

## What the briefing contains at every rung

Four items, none of which is the boundary itself, and all of which are cheap enough that no efficiency ranking applies to them:

- **Where to be, when, and what a slot means.** A slot is the commitment; anything beyond it is a courtesy that cannot be scheduled against.
- **How requests arrive** - the mechanic below, in the exact form participants will use it, so a mentor recognises a request when one appears.
- **Who to escalate to, on which channel**, for anything that is not a technical question: a distressed participant, a dispute inside a team, a safety concern.
- **The code-of-conduct escalation script.** Mentors are a named bound category under the event's code of conduct exactly as volunteers, judges and sponsors are, and any of them may be the first person a report reaches. `samber/dev-event-organizer-skills@event-code-of-conduct` owns the script, the pipeline and every sanction; the briefing points at it and never paraphrases it into a second version.

## The help-versus-build boundary

**A published precedent exists for the boundary itself, though not for the prize-eligibility reason behind it.** MLH's organizer guide asks for a mentor guide covering "questions to ask hackers, how to troubleshoot with the hacker, and useful beginner tools/resources", and stops short of the help-versus-build line itself. The Wikimedia Hackathon's own mentoring guide states that line as an absolute: "we do not touch their keyboard", and where typing on a participant's machine is genuinely unavoidable, "ask whether that is okay with them. And explain what you are doing."

Its reasoning is pedagogical rather than eligibility-driven: a Wikimedia Hackathon carries no prize to contest, so the rule there protects learning, not authorship. A judged, prize-bearing hackathon needs the same practice plus the eligibility reasoning above it.

The wording below adapts that boundary into this skill's own voice for a judged event, not a standard to cite. Present it that way.

Two reasons the line exists, and they point the same way:

- The event exists so that teams learn by building. A project carried by a mentor teaches its team nothing and wins over teams who were not carried.
- The submission has to survive judging. `samber/dev-event-organizer-skills@hackathon-brief-design` cites a published requirement that submitted code be public and stay public to remain prize-eligible, which means the commit history is readable and so is whose work it is.

A workable formulation, to be edited into the event's own voice:

> Mentors explain, diagnose, unblock and demonstrate. Mentors do not write code that ships in a submission. If a mentor needs to show something in code, it is written outside the team's repository - a scratch file, a message, a whiteboard - and the team types the version that lands. If a mentor has already committed to a team's repository, the team declares it in the submission.

The last clause matters more than it looks: a declaration turns a contestable finding at the ceremony into a disclosed fact before judging, which is the whole difference between a rule and a scandal.

**Positive example.** A team's deployment fails with an opaque error at hour twenty. The mentor reads the log with them, names the class of failure, asks what changed since it last worked, and points at the config file. The team finds and fixes the line. Elapsed time is longer than fixing it directly, and the team can now debug the next one.

**Negative example.** The same team, the same error. The mentor says "let me drive", takes the laptop, fixes the config, pushes, and moves on. The block is resolved in two minutes, the team cannot reproduce it, and the repository now contains a commit authored by someone who will possibly be in the room when prizes are decided.

## Power dynamics

The person asking is usually more junior, frequently exhausted, and often asking in front of their team. Three instructions, each with a published precedent behind it:

- **Ask before touching anything**: the laptop, the keyboard, the repository. Consent is the difference between help and takeover, and it is also how a mentor discovers the team has a reason for the thing that looks wrong. The Wikimedia Hackathon's mentoring guide states this as an absolute, quoted in full above, with one exception for genuinely unavoidable typing: ask first, then explain what you are doing as you do it.
- **Leave the project unevaluated.** A mentor's opinion of an idea reads as a verdict, especially from someone the team believes is close to the organizers. Judgement happens elsewhere, against published criteria. Garage48's mentor guide draws the same line from the team-autonomy side, "the hackathon is not about them and getting their ideas come to life, it's about helping the team", and extends it past the point a team can still act on a change: "hold yourself back and don't do it."
- **Name the limit of your own knowledge out loud.** "I have not used this" is more useful than a confident guess, and it is what triggers the escalation route rather than sending a team down an expensive wrong path. The Wikimedia guide lists the same habit first among its rules for a friendly mentoring atmosphere: "admit when you don't know something."

Where a mentor is also judging, the mentor-team contact log exists and the recusal rule belongs to `samber/dev-event-organizer-skills@hackathon-judging`. The briefing states the rule; it does not run it.

## The sponsor-representative engagement rule

One paragraph, given to every mentor supplied by a sponsor, and mirrored into the sponsorship commitment so it is a term rather than a surprise.

> While mentoring, you are helping the team in front of you with the problem they brought. Not a sales conversation, not a recruiting conversation, not a demo. If a participant asks about your company, your product's pricing, or working for you, answer briefly and point them at your booth or your team's channel. Wearing a mentor marker means a participant cannot easily decline the conversation, which is exactly why it is not the place to have it.

The instruction generalizes past the sponsor case. The Wikimedia Hackathon's mentoring guide states the general form as a flat rule for every mentor, sponsored or not: "we do not use the time to advertise our own companies/jobs/ourselves." What a sponsor-staffed mentor pool adds is a reason to write the rule down and hand it to the sponsorship relationship, rather than leave it as an unstated norm.

The compliance exposure this closes is disclosure: undisclosed, a participant who was pitched while asking for help surfaces it after the event, when the only remedies left are an apology and a difficult sponsorship conversation. Written down first, it costs a paragraph and is reversible next edition.

## The request mechanic, as participants read it

Publish it wherever the brief is published, before the window opens, and repeat it at the opening. A mechanic half the field never learns is a mechanic that serves the confident half.

Whichever rung you choose, state four things:

- **How to recognise a mentor** - the marker, the role name, or the location of the mentor area.
- **How to ask** - the channel, the sheet, the desk, or "walk up".
- **What to expect** - that somebody will come, roughly how a request is picked up, and that asking early is normal rather than an admission.
- **What happens when nobody covers it** - the escalation route, stated plainly, so a gap reads as a known limit rather than as being ignored.

Two published shapes worth copying directly:

- A channel where "mentors know where to look for requests", with organizers "active on chat" connecting hackers to mentors.
- A staffed mentor area: "a dedicated mentor area that hackers can walk up to with questions".

A third, "mentorship office hours: A peer group type of experience where people can ask questions during the assigned time", is a supplement to either. It answers questions that can wait, and a hackathon's blockers mostly cannot.

**Negative example, worth naming because it looks tidy.** A single organizer publishes "message me and I'll find you a mentor". It works all afternoon, produces a clean routing record, and stops at whatever hour that person sleeps - which at an overnight event is the hour with the most blocked teams and the fewest alternatives.

Any routing rung needs at least two people who can work it, or an unrouted fallback participants already know about.
