# Team size, composition rules and project supply

Contents: the adoptable size-rule wording · the solo question · composition rules and the protected-characteristic split · role coverage carried over from hardware events · project-backlog sizing · registration fields each mechanic needs.

## Table of Contents

- [The size rule you can adopt](#the-size-rule-you-can-adopt)
- [Deciding the number for your own event](#deciding-the-number-for-your-own-event)
- [Composition rules keyed to a track](#composition-rules-keyed-to-a-track)
- [Adjacent rules that bound what a formed team may do](#adjacent-rules-that-bound-what-a-formed-team-may-do)
- [Role coverage instead of headcount](#role-coverage-instead-of-headcount)
- [Project supply, and the one ratio that exists](#project-supply-and-the-one-ratio-that-exists)
- [What makes a stocked project actually joinable](#what-makes-a-stocked-project-actually-joinable)
- [Registration fields, by mechanic](#registration-fields-by-mechanic)

## The size rule you can adopt

The guide frames team size as one of several rules "your team should decide on" rather than a mandate. It poses the question as **"How many people per team?"**, with two sub-questions:

- **"Can hackers participate solo?"**
- **"Can the teams be as large as they want, and you might just have only 4 prizes?"**

It gives its own reason for capping: **"We see hackers have the most success with teams of a maximum size of 4"**. That is one organizer network's practitioner observation, with no study cited behind it. Present it as such when you recommend the number.

The copy-paste line the same source offers is **"Teams can be 1-4 people"**. The `1` is what makes it a range rather than a cap, and it is the whole reason to prefer this wording: it answers the solo question in the same five words that set the maximum.

A second, independent observation of the same quantity comes from civic-tech organizing: "Participants typically form groups of about 2-5 individuals". That is an observed range at such events, not a rule and not a target.

It overlaps the 1-4 cap without contradicting it: a cap of 4 and an observed range of 2-5 disagree only at the top, and only for teams the cap would have refused anyway. Do not average the two numbers into a third one; they are answering different questions.

## Deciding the number for your own event

The rule has to compose with three things decided elsewhere, so check each before publishing:

1. **The prize medium.** A non-cash prize counted in whole units per person divides very differently against a cap of four than cash does. `samber/dev-event-organizer-skills@hackathon-cash-prize` owns that; get the medium before you get attached to a cap.
2. **The mechanic.** A facilitated mixer that forms groups of six under a cap of four wastes the session and the room. Whatever mechanic is chosen has to be told the cap before it runs.
3. **The judging window.** A smaller cap means more teams for the same headcount, which is the input `samber/dev-event-organizer-skills@hackathon-judging` uses to work out whether every team can be seen at all. A cap decision is a team-count decision wearing different clothes.

State the rule as a range including the minimum, always. "Maximum 4" leaves the registration desk answering the solo question one person at a time.

## Composition rules keyed to a track

Some tracks require a fraction of the team, not just the individual, to meet a category. The published example is **"Beginners track where at least half the team must have this be their first hackathon"**. The procedural instruction beside it is the part that actually prevents damage: **"Make sure to state how many of the team members must fit the category in your rules before the event."**

Decide the fraction; treat "half" as one event's number rather than a standard.

**Split composition rules into two kinds before writing either.** The split is this skill's own construction, not a distinction any published rule set draws.

- **Experience-keyed** - "first hackathon", "no prior submission to this event", "fewer than N years writing code". A self-reported skill-level proxy. It reviews cheaply, it is reversible next edition, and a wrong call costs a track's fairness rather than anything more.
- **Characteristic-keyed** - age, student status, employment, gender, disability, nationality. This is a carve-out on a protected characteristic.
  - It needs review before publication by whoever owns that exposure for the organizing entity.
  - The eligibility question it creates has to be asked and stored under a stated data posture.
  - It cannot be withdrawn mid-event without re-scoring the track.
  - Route it rather than wording it yourself, and take the data-handling half to `samber/dev-event-organizer-skills@event-accessibility-inclusion`, which already sets a posture for sensitive attendee data.

Whichever kind, write down what happens to a team that stops satisfying the fraction after somebody leaves. That is a mid-event change question and a composition question at the same time, and it is only cheap while it is hypothetical.

## Adjacent rules that bound what a formed team may do

These are not team-formation decisions, but each one constrains the roster you publish:

- **"Organizers, volunteers, judges, sponsors, or anyone in any other privileged position at the event should not participate as a hacker."** The copy-paste version softens it usefully - such people may build for their portfolio but are not prize-eligible. That is who may join a team at all, and it is worth saying before the mechanic runs rather than after somebody has spent a day on a project.
- **"All team members should actively participate in the event."** A membership rule stated as an expectation, with no mechanism behind it. Do not build an enforcement process out of it.
- One project per team, entered into as many challenge tracks as apply. That pair is what makes challenge tracks cheap and is owned by `samber/dev-event-organizer-skills@hackathon-brief-design`; it matters here only because it means a track does not split a team.

## Role coverage instead of headcount

A hardware-hackathon guide invites participants to form multidisciplinary teams "to allow for efficient parallel creation", because coding, fabrication and design must happen simultaneously. Four core roles are named:

- **Programmer** - microcontrollers, firmware, API integrations.
- **Fabricator** - wiring, 3D printing, laser cutting, component assembly.
- **Designer** - UI/UX, structural planning, materials and crafting.
- **Visionary** - storytelling, time management, presentation, named explicitly as project management.

Plus one duty attached to no headcount slot: **"Someone on the team should also be focused on quality assurance and debugging."**

A software-only hackathon has no fabricator, and these roles are that build's physical dependencies enumerated. Three things carry over to a software event, on this skill's own reasoning rather than on any measurement:

1. **Functions, not headcount.** Four roles and a cap of four is a coincidence, not a design. Keep the function list and the size rule independent so a three-person team can cover four functions and a four-person team can leave one uncovered on purpose.
2. **The cross-cutting duty.** At least one responsibility should be named as everyone's rather than someone's. In the source it is QA and debugging; whatever yours is, name it the same way.
3. **The recruiting consequence.** The same file asks organizers to market explicitly to people who "may not self-identify as 'hackers'" - designers, artists, scientists, writers - with the stated reason that hardware needs a "whole product" approach rather than just code. The point that carries across is upstream of any mechanic: a coder-only applicant pool makes skill balance impossible at matchmaking time no matter which mechanic you picked. The pool is decided months before the pitch round.

Your own function split for a software event is **yours to write**. Borrow the shape - three or four named functions plus one shared duty - not the list itself.

## Project supply, and the one ratio that exists

At themed hackathons, "ensure that there is at least one subject matter expert + workable project for about every four non-expert participants."

Carry all four bounds every time this number appears:

- **Themed events only** - events where projects are confined to one problem domain. It says nothing about an open-theme hackathon.
- **Counts non-expert participants** - not teams, not registrations, not attendees.
- **The expert is a domain expert embedded in one project**, not a floating technical helper: the same passage notes such a person "can only effectively participate in a single project during the event".
- **It is a project-supply target**, and its stated purpose is to prevent the room splitting off a third group of "participants struggling to find something relevant to work on".

`samber/dev-event-organizer-skills@hackathon-mentoring` carries the fuller scoping and a standing warning never to convert it into mentors per participant. Use it here only as a sizing prompt for how many pitchable projects a themed event needs.

## What makes a stocked project actually joinable

The same source's project-quality checklist, verbatim in its headings: **Clearly articulated** (a clear question or problem plus a reasonably specific proposed solution), **Attainable**, **Easy to onboard newcomers**, **Led by a stakeholder**, **Organized**. Two of the five are load-bearing for team formation specifically:

- **Easy to onboard newcomers** - "Projects should have ready-to-go tasks for newcomers with a variety of skills and at a variety of skill levels". Coding tasks should not require intimate knowledge of the codebase, and the build environment should be one that "can be spun up in less than 20 minutes". That 20 minutes is the practical test of whether a listed project is joinable or merely listed.
- **Organized** - "For projects with four or more members, especially newcomers, the project leader's role should be to coordinate, ensuring each team member has something to work on and helping to welcome new team members." Note the threshold is four _or more_, and note that it makes joining a team someone's explicit job rather than an emergent property.

The source also names the pre-event step that makes routing possible at all: meet project leads beforehand to "Identify how they can take on newcomers, what tasks are doable for newcomers" and "Identify what sort of help their project needs".

## Registration fields, by mechanic

Only collect what a named person will read. Each field below is what a specific rung actually consumes.

| Field                                                 | Which rung needs it                                                       | Note                                                                                                                                 |
| ----------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| Registering alone or as a team, and teammates' names  | Every rung, the matchmaking delete condition, the change-policy promotion | The single most useful field on the form                                                                                             |
| "Are they new to hackathons?"                         | Composition rule, mixer promotion, escalation promotion                   | Published as a registration-form question                                                                                            |
| Role or kind of participant                           | Pre-event matching, role coverage                                         | Published examples: "Developer. Designer. Data Scientist. Domain Expert. Government Staff. Communicator. Project Manager. Advocate." |
| "What are they interested in hacking on?" (free form) | Pre-event matching, backlog stocking                                      | Published as a form field; free text, so somebody has to actually read it                                                            |
| Bringing a project to pitch                           | Pitch round sizing, backlog sizing                                        | Tells you whether the round has enough pitchers to absorb the room                                                                   |
| Access or accommodation needs                         | Mechanic accessibility                                                    | Owned by `samber/dev-event-organizer-skills@event-accessibility-inclusion`; never a team-formation field                             |

The same source's instruction on why to gather any of it: "Literally try to imagine how each registered participant will keep occupied at the event based on whatever information you know about them." A field nobody performs that exercise against is a field that costs the registrant time and buys nothing.
