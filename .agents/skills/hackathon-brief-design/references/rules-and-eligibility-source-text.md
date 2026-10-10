# Rules and eligibility: the adoptable set, its edits, and what it does not cover

Contents: the two source documents · what the set already covers · the fields you must edit · eligibility definitions · carve-out mechanics · the pre-existing-work rules · AI disclosure · intellectual property · what to check before adopting.

## Table of Contents

- [Why adopting beats drafting](#why-adopting-beats-drafting)
- [What the set already covers](#what-the-set-already-covers)
- [The fields you must edit before publishing](#the-fields-you-must-edit-before-publishing)
- [Eligibility definitions worth borrowing](#eligibility-definitions-worth-borrowing)
- [Carve-out mechanics](#carve-out-mechanics)
- [Intellectual property and ownership](#intellectual-property-and-ownership)
- [Check these before adopting anything above](#check-these-before-adopting-anything-above)

## Why adopting beats drafting

The source states its own licence to fork: _"Organizers can use these rules exactly or fork them and edit them to suit their needs."_ It also states the value being bought, in the organizer guide's own framing: clear rules help _"easily be able to point to specific rules if you have to disqualify hackers for cheating."_

That is the whole argument for the default rung in SKILL.md's rules menu. You are not saving writing time so much as inheriting the disputes somebody else already had.

## What the set already covers

Team and participation:

- _"Teams can be 1-4 people"_ - the copy-paste default. The 1 keeps solo entry open.
- The stated rationale, presented by the source as its own observation rather than as a study: _"We see hackers have the most success with teams of a maximum size of 4"_. Present it that way; it is a practitioner claim about team success, with no measured basis given.
- _"Organizers, volunteers, judges, sponsors, or anyone in any other privileged position at the event should not participate as a hacker."_
- _"All team members should actively participate in the event."_

Submission scope:

- _"You may not submit multiple projects"_ and _"You may submit to multiple challenges"_ - the pair that makes challenge tracks cheap. One project, many track entries.
- _"You may not crosspost to other hackathons"_ - the guide notes it generally recommends no but that the organizer should state it either way.

Work window and originality:

- _"You may not work on your project before the event. You must stop development when submissions are due"_, with the consequence spelled out: _"This means that you should not be reusing code from previous projects."_
- _"Teams can use an idea they had before the event."_
- _"Teams can work on ideas that have already been done. Hacks do not have to be "innovative"."_
- _"Teams can work on an idea that they have worked on before (as long as they do not re-use code or other project materials)."_
- _"Teams can use libraries, frameworks, or open-source code in their projects. Working on a project before the event and open-sourcing it for the sole purpose of using the code during the event is against the spirit of the rules and is not allowed."_
- _"Teams must stop hacking once the time is up. However, teams are allowed to debug and make small fixes to their programs after time is up."_ The source draws the line at a few lines of code; large changes or new features are not allowed.

The idea/code split is the useful structure here: an idea may be old, the code may not. That distinction is what lets a rule ban pre-built work without banning returning teams.

Code and artifacts:

- _"Your code must be available publicly (ideally in a git repository)."_
- _"Your code must remain public post event to be eligible for prizes. If your repo and video are not public, new winners may be selected."_
- The organizer guide's copy-paste version names a submission platform inside the sentence (_"You must submit your code in your Devpost submission as a publicly available link(GitHub link, repl.it, Google Drive link, etc). You should remove any API keys before sharing."_). The platform name is part of the quote, not a recommendation - the transferable requirements are a public link and key removal.
- _"You may use publicly available frameworks, but you need to list said frameworks in a readme."_

AI tooling, from the source's own dedicated section:

- _"Teams may use AI to assist them while coding, utilizing tools such as code completion, code generation, image generation, or other similar tools."_
- _"Teams should be honest and transparent about the AI code tools they used. This includes listing them in their project submissions and answering questions when talking to organizers, judges, and other hackers."_
- The organizer guide adds the enforcement half: a project should not be _"a reskin of an existing AI tool"_, and undisclosed use is grounds for disqualification and a report to the network.

Enforcement:

- _"Teams can be disqualified from the competition at the organizers' discretion."_ The next sentence gives a non-exhaustive grounds list: breaking the competition rules, breaking the code of conduct, or other unsporting behaviour.

## The fields you must edit before publishing

The source names three of these itself; the rest follow from the text. Adopting without editing them is the common failure, because the set arrives carrying another event's decisions.

1. **Team size.** The number is yours. Publish it and the solo-entry answer together.
2. **Hacking start and end times.** The source explicitly recommends adding _"specific start/end times for hacking"_ - the set ships without them.
3. **Eligibility scope and age.** The source states outright that _"The copy paste does not include age limit or demographic specific challenges."_ Its own copy-paste line reads _"This event is limited to students"_, which is wrong for most non-student events and must be replaced rather than deleted.
4. **Judging mode.** Its copy-paste line reads _"Judging will be in person."_ Replace it with whatever the format skill actually chose.
5. **Crossposting.** Decide yes or no, and state it either way.
6. **Public-code requirement.** Keep it only if your event can honour it. A corporate or client-data hackathon frequently cannot.
7. **The code-of-conduct link.** Point at your own document.
8. **The disqualification-reporting channel.** The source names its own network's address; yours must be a person or address you control.

## Eligibility definitions worth borrowing

The set defines "student" broadly rather than leaving it to interpretation, which is the transferable move:

> _"MLH Hackathons are primarily for students, but may also include professionals. MLH chooses to define students broadly. Anyone who attends a traditional school, college, or university, and those in bootcamps and similar programs are students. Those who graduated within the last 12 months are also considered students."_

Two things to copy: define every eligibility word that could be argued, and pick a numeric boundary where one exists (here, twelve months since graduation) rather than a vague one. Two things that do not transfer: the student framing itself, and the choice of twelve months, which is that network's number and not a standard.

## Carve-out mechanics

A demographic or skill-level challenge track composes with the base rules and needs its own wording. The procedural rule below is published; the fraction is yours to set.

- The published example: _"Example: Beginners track where at least half the team must have this be their first hackathon."_
- The rule that matters more than the example: _"Make sure to state how many of the team members must fit the category in your rules before the event."_

A carve-out needs four sentences to be enforceable:

1. who qualifies
2. what fraction of the team must qualify
3. how it is evidenced at submission time
4. what happens to a team that becomes ineligible when a member joins or leaves

The fourth is the one that gets skipped and the one that produces the argument.

## Intellectual property and ownership

The MLH rule set carries **no intellectual-property clause, no ownership assignment and no originality warranty.** Its only ownership-adjacent requirement is that submitted code be public and stay public to remain prize-eligible - which constrains visibility, not ownership.

Who owns work submitted to a hackathon is a jurisdictional legal question, not something a brief template settles. This skill therefore:

- writes no IP clause and recommends none;
- routes any sponsor request for IP terms to counsel via `samber/dev-event-organizer-skills@event-sponsor-agreement`, named in SKILL.md;
- treats "the participants keep their work" as an assumption to confirm, not a fact to publish.

If a brief needs an IP sentence, that sentence comes from a lawyer who has read the sponsorship agreement and the local law. A plausible-sounding paragraph written here would be the exact defect this skill was built to avoid.

## Check these before adopting anything above

- Does your event's culture actually permit public code?
- Is anyone under the age of majority expected, and does that change consent, prizes or photography?
- Does a sponsorship agreement already impose terms that contradict a rule you are about to publish?
- Does the code of conduct you link to have a working reporting channel and a named responder?
- Is any personal data collected at submission time, and does the brief say what happens to it?

Each one is a question to route, not a rule to write.
