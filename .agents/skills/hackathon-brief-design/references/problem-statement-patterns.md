# Problem statements: the four rungs, the skeleton, and sizing to the time box

Contents: the four rungs in detail · the structured-statement skeleton · a worked pair (weak and rewritten) · a real published example · sizing ambition to the time box · themed-event capacity planning · wording a sponsor prompt differently from a general one.

## The four rungs in detail

**Open theme.** One phrase - "build something with AI", "civic data". Correct only where participants arrive with their own problems and pitch them to each other; the brief's job then is to publish the pitch mechanic and the rules, not the problem. Deleted outright once a sponsor funds a prize, because the prize needs a criterion.

**Domain-scoped prompt.** A theme plus a bounded domain: "build a tool that helps a small city publish its budget data". Cheap, real segmentation, and it still leaves the in-scope question open - which is precisely the question an organizer then answers forty times during the event.

**Structured statement.** The default. Context, the challenge as a question, constraints, resources provided, and a minimum viable submission. Skeleton below.

**Fully specified challenge.** A structured statement plus a starter repository, a dataset, and acceptance tests a submission either passes or does not. Organizer engineering work measured in days.

It buys near-perfect comparability, and it changes the event: teams execute a specification rather than choose a problem. Reserve it for long time boxes or for a sponsor whose integration tests already exist and can be handed over unchanged.

## The structured-statement skeleton

Five parts. A prompt missing any one of them is a domain-scoped prompt wearing a structured statement's formatting.

1. **Context** - two or three sentences on who has this problem and why it is not already solved. This is what stops teams building for a user who does not exist.
2. **The challenge, as a question** - one sentence, ending in a question mark. If it cannot be phrased as a question, it is a topic rather than a challenge.
3. **Constraints** - what the submission must do, must not do, and what platform or data it must work with. Keep these to things a judge could actually check.
4. **Resources provided** - datasets, API access, sample content, mentor availability, and where each is obtained. Name what is _not_ provided too; teams plan around absences they know about.
5. **Minimum viable submission** - the smallest thing that counts as a real entry. This is the part most briefs omit and the part that most changes team behaviour.

A good hackathon project is clearly articulated - a clear question or problem to solve, plus a reasonably specific proposed solution - and attainable, with easy onboarding for newcomers. Apply the twenty-minute onboarding test to any starter repo: the build environment can be spun up in less than 20 minutes.

That standard assumes participants pitching their own projects. When the organizer writes the prompt instead, every one of those qualities becomes the organizer's burden rather than the participant's.

## A worked pair

Both invented, and neither is an archived brief from a real event. The weak version is the shape a first draft usually takes.

**Weak - a topic formatted as a challenge:**

> **Sustainability Track.** We are excited to see what you build in the sustainability space! Use technology to make the world greener. Bonus points for creativity and impact. Best project wins.

What is wrong with it:

- no user
- no question
- no constraint a judge could check
- no resource
- no floor

"Bonus points" also leaks a weighting decision that belongs to `samber/dev-event-organizer-skills@hackathon-judging`, and "impact" names a category the brief never defined. Three teams will build carbon calculators and nobody will be able to say which one answered the prompt.

**Rewritten as a structured statement:**

> **Sustainability track - reducing waste in shared kitchens**
>
> _Context._ Shared kitchens in co-working spaces and student residences throw out food that is still good, mostly because nobody knows what is in the fridge or whose it is. The people running these spaces manage them by hand, on paper or in a group chat.
>
> _The challenge._ How can a small shared kitchen see what food it already has, and get it used before it spoils?
>
> _Constraints._ It must work for a space with no dedicated staff and no budget for hardware beyond a phone. Assume the people using it will not create accounts. Do not assume a network connection in the kitchen itself.
>
> _Resources provided._ An anonymised eight-week inventory log from two residences, and two mentors with facilities-management experience on Saturday afternoon. No barcode database - sourcing one is part of the problem.
>
> _Minimum viable submission._ Something a person can put one item into and get one useful signal back out of. A working single-flow demo counts. A design with no running code does not.

Why it works: a team can decide in one reading whether it wants this problem, what "done enough" means, and what it will show a judge. The constraints are checkable, and the absent resource is named.

## A real published example

NASA's Space Apps Challenge publishes a structured brief per challenge, archived by year at spaceappschallenge.org. The 2024 challenge "Leveraging Earth Observation Data for Informed Agricultural Decision-Making" ([spaceappschallenge.org](https://www.spaceappschallenge.org/nasa-space-apps-2024/challenges/leveraging-earth-observation-data-for-informed-agricultural-decision-making/)) runs a one-paragraph summary followed by three labelled sections: Background, Objectives, Potential Considerations.

> **Background** (excerpt). "Farmers around the world grapple with a myriad of challenges that threaten their livelihoods and food security... Innovative solutions that empower farmers to leverage data-driven insights from trusted resources - including NASA Earth data - are needed to tackle challenges related specifically to water."

> **The challenge, as published.** "Your challenge is to design a tool that empowers farmers to easily explore, analyze, and utilize NASA datasets to address these water-related concerns and improve their farming practices."

> **Potential Considerations** (excerpt). "You may (but are not required to) consider the following" NASA datasets grouped by topic - precipitation (IMERG), floods (OPERA, SAR), drought (Landsat, ASTER, ECOSTRESS), soil moisture (SMAP), and more - alongside open-ended concept prompts such as "Data Detective" and "Data Dashboard."

Measured against the five-part skeleton above:

- **Context** - present, and longer than "two or three sentences": Background runs four paragraphs and names why the problem persists ("past efforts... often struggled due to lack of a 'farmer-centric' design, limited stakeholder engagement, and inadequate scalability").
- **The challenge, as a question** - absent. NASA states it as an imperative, repeated near-verbatim in the summary and again inside Objectives, never as a question.
- **Constraints** - absent as a checkable bar. Potential Considerations is explicitly optional ("You may... consider") and reads as inspiration a team can take or leave, not a rule a judge could hold a submission against.
- **Resources provided** - present and itemized by topic, with a further Resources tab linking the full dataset and tool list.
- **Minimum viable submission** - absent. The brief widens the format instead of narrowing it: "You could make a storyboard, slide deck, a prototype, or something else."

Two of five parts are missing, and the two present run heavier than the skeleton's own weight budget. Read this as evidence for what the floor buys rather than a counter-example to it: NASA's challenge is judged across hundreds of local events by volunteer judges grading open-ended entries, where a wide format and no stated floor is a deliberate choice, not an oversight. An organizer without that reviewing capacity, or running one event rather than hundreds, needs the floor the skeleton asks for to keep entries comparable.

Smart India Hackathon runs the opposite shape. Its published problem statements (compiled from the official portal, e.g. a mirror at [gist.github.com](https://gist.github.com/VidhyaVarshanyJS/ee558d70f6278ee8a6c6e1375952e4ca)) carry six fields per entry - ID, title, technology bucket, category, description, submitting organization. Context, the ask and any constraint fold into the one Description field rather than separating out, and neither Background nor Expected Outcomes gets a column of its own.

Problem statement SIH1524, submitted by the Indian Space Research Organisation, states its ask inside that single field: "a secure DNS resolver that blocks resolution of malicious domain names" using threat-intelligence feeds, with "average DNS lookup time within 100 milliseconds". That is a real, checkable number delivered without a labelled Constraints section.

A reader still gets the skeleton's content, compressed into fewer and denser fields. The five-part shape is one way to carry this information, not the only way a real organizer has shipped it.

## Sizing ambition to the time box

One practitioner observation exists: _"Most projects will accomplish about 25% of what they think they can accomplish in the limited time they have."_

Name the quantity exactly. That is **25% of a team's own pre-hack estimate of its own scope** - a ratio of what a team delivers to what that same team predicted it would deliver. It is not a ratio against the organizer's prompt, not a completion rate against any external standard, and not tied to any stated duration.

The mapping from specificity to duration below is reasoned rather than measured. Apply it to your time box:

- **Under ~24 hours** - the minimum viable submission has to be genuinely small, and the resources have to be ready to use rather than ready to find. A team that spends four hours getting data is a team that spends a sixth of the event not building.
- **A weekend (~36-48 hours)** - the structured statement's natural home. Enough time for a team to choose an angle within the prompt, which is where the interesting divergence between submissions comes from.
- **A week or longer** - the fully specified rung becomes affordable and, for comparability, worthwhile. Teams have time to read acceptance tests and still build.

The one instruction that holds at every duration: name a minimum viable submission. It is what makes the quarter a team actually reaches into something demoable.

## Themed-event capacity planning

For a themed event where domain experts supply the problems, one ratio is worth knowing: maintain **one expert-plus-workable-project pairing per four non-expert participants.** It counts participants, not teams, and it counts expert-project pairs, not challenge tracks - do not silently convert it into a tracks-per-team ratio, which is different.

A themed event without enough workable projects splits into three groups, and the third is the failure the ratio exists to prevent: participants _"struggling to find something relevant to work on"_. The ratio comes from civic-tech events, where subject-matter experts arrive with domain problems that need translating into buildable projects. At a sponsor-API hackathon the equivalent expert is the sponsor's own engineer, and the ratio's purpose shifts from translating problems to unblocking integrations.

## Wording a sponsor prompt differently from a general one

A sponsor prompt has one extra job: it has to be a problem, not a product tour. Three differences from a general prompt:

- **State the constraint as a capability, not a brand instruction.** "The submission must persist data across sessions using the provided hosted store" beats "must use Sponsor X". The first is checkable and buildable; the second reads as an advertisement and produces integrations bolted on at hour thirty.
- **Publish the access path in the prompt itself** - where keys come from, what the rate limit is, and who to ask when it fails. Access friction is the single largest determinant of whether a sponsor track gets entries.
- **Publish the fallback.** State what happens to a team whose sponsor integration is unavailable through no fault of its own: judged on design and integration plan rather than a working call. Written in advance it is a rule; improvised during the outage it is a favour, and every team that already finished will say so.

One more, from the general rules rather than the sponsor: because a team may enter one project into several challenge tracks, a sponsor prompt should say explicitly that entering it does not forfeit the general prize. Teams assume the opposite, and the assumption suppresses entries.
