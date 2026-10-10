# Coverage sizing, expertise tagging and recruitment

Contents:

- How coverage is sized against tracks rather than headcount.
- The one published ratio and everything that bounds it.
- Expertise-tag vocabulary.
- Recruitment channels with their lead times.
- The sponsor channel as a deliverable.
- The escalation route when no mentor covers a skill.

## Size coverage against the brief, not against the room

What runs short at a hackathon is mentor-hours _of the right kind, in the hours that kind is needed_, never mentor-hours in aggregate. A generously staffed roster holding nobody who has used the sponsor's SDK leaves one track unsupported while looking healthy on a headcount.

Work in this order, and stop at whichever step the event's scale makes sufficient:

1. **List the coverage units from the published brief.** One per challenge track, plus one per named sponsor technology, plus one for the general stack every team touches regardless of track (version control, deployment, whatever the event's dominant language is).
2. **Set a commitment level per unit**, not a number. Three levels are enough: _unstaffed and published as such_, _available_ (somebody on the roster holds the tag, reachable through the request mechanic), _guaranteed_ (somebody holding the tag is present for stated hours).
3. **Derive the guarantee from the brief's own sponsor-integration depth.** An optional bonus challenge needs _available_. A technology required for one track, or standing as the event's sole substrate, needs _guaranteed_: the brief's published fallback sentence promises those teams a route, and an unstaffed guarantee turns that promise into an improvisation during an outage.
4. **Hand the guaranteed hours to the shift plan as a constraint.** The grid arithmetic, the arrival buffer and the backfill protocol belong to `samber/dev-event-organizer-skills@event-volunteers`; what you hand over is "this tag, present, these hours".

Size the roster by coverage units, not by a headcount ratio: a ratio tells you nothing about which tag is unstaffed at 03:00.

## The one published ratio, with every bound it travels with

Joshua Tauberer's `hackathon.guide`, in a section headed _At Themed Hackathons_, states:

> ensure that there is at least one subject matter expert + workable project for about every four non-expert participants

Four bounds, and none of them is optional when the figure is quoted:

- **Themed events only.** The source defines a themed hackathon as one "in which the projects are confined to a particular problem". It says in the same passage that open-ended events are "not good at" attracting these experts at all.
- **It counts non-expert participants.** Not teams, not registrations, not attendees.
- **The expert is a domain expert inside a project, not a floating mentor.** The same sentence says such a person "can only effectively participate in a single project during the event": a participant with a specialism, occupied, rather than a roving helper. The unit being counted is "expert + workable project" as a pair.
- **The purpose is project supply.** The stated failure it prevents is the room splitting into three groups, the bad one being "participants struggling to find something relevant to work on".

What it is legitimately good for: evidence that _expertise_ rather than headcount is the scarce quantity, and a reason to count coverage units before counting people. What it must never become: mentors per participant, at any event, in any form. A staffing formula derived from it would be a number with none of its four bounds attached.

## Expertise-tag vocabulary

Tags do routing work or they do nothing, and the failure mode without them is observed rather than hypothetical. Hack4Bengal's organizers describe manual assignment producing exactly this mismatch: "half the time, a team building a computer vision project gets a mentor whose expertise is frontend frameworks." Their fix tags mentors by technology and depth - "LangChain (advanced), Neo4j (expert)" - and matches against the stack a team's project actually uses.

Three rules:

- **Use the brief's own words.** If the brief names an "accessibility track", the tag is "accessibility" - not "frontend". A tag a participant cannot map to the track they entered cannot route their request.
- **Name sponsor technology explicitly, by product name**, and separately from the general skill it sits inside. "Knows the sponsor's SDK" and "knows the language it is written in" are different claims and different coverage.
- **Record depth honestly, in two levels: has built with it, or has read the docs.** A roster full of the second kind reads as covered and is not. Ask at sign-up rather than inferring from a job title.

Keep the tag list short enough that a coordinator can hold it in their head at three in the morning. A taxonomy nobody can recall under pressure routes nothing.

## Recruitment channels

**Community and alumni.** MLH's organizer guide states: reach "existing tutors or teaching assistants to mentor because they already find the value out of mentoring and teaching others", then "alumni from your school, industry professionals, and your professors". The same source asks organizers to "set up a webpage mentioning a mentor's responsibilities" and "a form to get the interested people to register, asking them to pick a timeslot".

The setting is what fails to transfer. That channel assumes a university with a department, a TA pool and an alumni list behind it. A community or corporate hackathon substitutes a user group, a past-participant list, a partner company's engineering team, or the maintainers of a library the brief depends on: the shape of the ask survives, the population does not.

**Sponsor technical staff.** Not a volunteer sign-up and not a recruitment channel in the ordinary sense: it is a sponsorship deliverable, and it is negotiated in `samber/dev-event-organizer-skills@event-sponsor-agreement` before it reaches this plan. Three things belong in that agreement rather than in a briefing email:

- Named people, or at least a named count and the technology they cover.
- The hours they are committed for, matched against the hours their track is worked.
- The engagement rule - that they are there to unblock teams, not to pitch or recruit - so that briefing them on it later is a reminder rather than a new condition.

A sponsor mentor who does not appear is a fulfilment failure against a commitment, handled with the sponsor, not a volunteer no-show handled with a backfill.

**Returning mentors.** The cheapest channel that exists, and it only exists if somebody kept the list. `samber/dev-event-organizer-skills@event-volunteer-experience` describes the same asset for a different population, with its owner-and-deletion-date rule; apply that rule here rather than inventing a second one.

**Lead times.** Set the mentor recruitment lead time from a real deadline rather than a calendar rule of thumb. That deadline is not the event date: it is whichever comes first of the briefing session, the shift grid's close, and any badge or marker order, the same logic `samber/dev-event-organizer-skills@event-volunteers` applies to a volunteer roster.

Work backwards from that date.

## When no mentor covers a needed skill

Publish the route before the window opens, because improvising it during the window is how a team is told "ask around" and hears "no".

1. **Check the roster's second level first.** Someone who has read the docs beats nobody, provided that person tells the team plainly which level it is getting.
2. **Ask the sponsor's channel** where the gap sits on the sponsor's own technology; that is what the commitment was for.
3. **Ask the room.** A participant on another team frequently holds the skill, and helping is not competing. Keep it a request, never an assignment, and attach no obligation to it.
4. **Tell the team plainly that no help exists for this**, early enough for them to change approach. A team told at hour four can pivot; a team told at hour thirty cannot.

Record every one of these as a coverage gap against a track. It is the single most useful thing next edition's plan inherits.
