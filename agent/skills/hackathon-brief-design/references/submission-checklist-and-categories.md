# Submission checklist, deadline mechanics, and evaluation categories

Contents: how the demo format drives the checklist · the artifact list with wording · deadline and late policy · evaluation categories · the exclusion list.

## How the demo format drives the checklist

The demo/judging structure is chosen upstream and is a fixed input. It decides which artifact carries the judging, and the checklist follows from that rather than from a template.

| Chosen structure                               | What carries the judging                    | What the checklist requires                                                                                    | What it must not require                               |
| ---------------------------------------------- | ------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| Expo table (judges rotate through tables)      | The live demo at the table                  | Code link, readme, a one-paragraph description, and the team present at its table for the whole window         | A video, unless it is genuinely optional and says so   |
| Stage pitch (teams present to a panel)         | The live presentation                       | Code link, readme, description, plus whatever the stage needs - slide deck by a stated time, a named presenter | A separately judged video that competes with the pitch |
| Video only (async judging)                     | The submitted video and written description | Code link, readme, and a video whose length, content and availability constraints are all binding              | Attendance at any live slot                            |
| Hybrid (expo round narrows to stage finalists) | Both, in sequence                           | The expo checklist for everyone, plus what finalists must add and by when                                      | A finals artifact due before finalists are known       |

The recurring mistake is a brief that requires a video at an expo-table event. Nobody watches it, teams spend an hour of the time box making it, and it becomes an unjudged obligation the rules still allow a team to be disqualified over.

## The artifact list, with wording

The requirements below are quoted or paraphrased from MLH's published rules. Each is stated in the source as an absolute requirement, not a suggestion.

**Code link:** _"Your code must be available publicly (ideally in a git repository)."_ And the durability half, which is the one teams break: _"Your code must remain public post event to be eligible for prizes. If your repo and video are not public, new winners may be selected."_

Say both halves in the brief. A team that makes its repository private the Monday after has broken a rule it never read.

Adoption check: the public-code requirement assumes a culture that is open by default. A corporate hackathon on internal data, or a client-confidential event, cannot honour it. Decide that before copying the sentence - and if you drop it, say what replaces it, because "submit your code" with no visibility rule is what produces a submission nobody can open.

**Key hygiene:** Remove API keys before sharing the link. Worth restating as a positive instruction rather than a prohibition - teams leak keys by forgetting, not by intending.

**Readme:** _"You may use publicly available frameworks, but you need to list said frameworks in a readme."_ The readme is also the cheapest place to require what the project does in two sentences, how to run it, and what is not finished. Judges read it when a demo fails.

**AI-tool disclosure:** _"Teams should be honest and transparent about the AI code tools they used. This includes listing them in their project submissions and answering questions when talking to organizers, judges, and other hackers."_ The enforcement half - a project should not be a reskin of an existing AI tool, and undisclosed use is grounds for disqualification.

This is the fastest-moving rule in any current brief. Write it as a disclosure requirement rather than a usage ban; a ban is unenforceable and a disclosure requirement is checkable in the readme.

**Demo video**:

- two minutes or less
- created during the event
- naming the hackathon at the start of the video
- remaining public afterwards to stay prize-eligible

The naming requirement exists to stop a video being reused across events, which is worth explaining in the brief so it does not read as bureaucracy. For fully digital events, this is the binding deliverable that carries the judging.

**Written description:** one paragraph, in the team's own words, saying what the project does and who it is for. It is what a judge reads before arriving at the table and what a sponsor reads when picking its own track winner.

## Deadline and late policy

- Set the deadline as a **wall-clock time in a named time zone**, never as "the end of hacking" or a duration. Teams in different rooms disagree about when hacking ended; nobody disagrees about 11:00 CET.
- Set it **before** the first judging slot, not at it. Judges need the submission list to exist before they start moving.
- State the late policy **before anyone is late**. Whatever it is (a hard cut, a grace window, a reduced eligibility) must be a rule in advance, not a favour afterwards.
- State what counts as submitted: the artifact exists at the link, and the link is reachable. A team that submitted a private repository has not submitted.
- The one published allowance worth mirroring: MLH's rule set permits debugging and small fixes after time is up but not new features or large changes. If you allow that, say where the line is; if you do not, say that instead.

## Evaluation categories

Name them in the brief. Do not weight, scale or total them - that belongs to `samber/dev-event-organizer-skills@hackathon-judging`, and a weight published here becomes a promise it has to honour or contradict.

One published set exists, each category with a written description of what a judge is looking for:

- **Technology**:
  - how technically impressive the hack is
  - whether the problem tackled was difficult
  - whether a clever technique or many components were involved
- **Design** - whether the team thought about the user experience, and how well made the interface is, adapted per project type (visual craft for a website, interaction quality for hardware).
- **Completion** - whether the hack works, and whether the team achieved what it set out to.
- **Learning** - whether the team stretched itself and tried something new, explicitly rewarding a team that switched away from what it always builds.

Deliberately do not carry weighting into the brief; hand it to `samber/dev-event-organizer-skills@hackathon-judging` as an input, since the weighting decision is theirs.

Adapt the set rather than copying it. These four fit a learning-oriented student event - "learning" in particular rewards a behaviour a commercial hackathon may not care about. A corporate or sponsor-led event judging commercial viability needs a different list, and inventing categories that match the prizes is legitimate; inventing weights is not.

## The exclusion list

A published evaluation set explicitly excludes what its criteria do **not** include, with reasons:

- code quality
- pitch quality
- idea quality
- how well the project solves a real problem

One stated reasoning: _"How well you pitch. Hacking is about building and learning, not about selling."_

Publishing an exclusion list is the cheapest way to stop teams optimising for something nobody is scoring. Teams read the brief as a specification of how to win, and an unmentioned dimension gets guessed at.

Copy the shape, decide the content yourself. This particular list belongs to a learning-oriented event; an event that genuinely does judge commercial viability would exclude a different set, and copying these four exclusions unchanged would contradict its own prizes.
