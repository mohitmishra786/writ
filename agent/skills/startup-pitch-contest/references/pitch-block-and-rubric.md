# Pitch block and rubric

## Contents

- Consuming the allocation arithmetic without re-deriving it
- The observable / claimed split
- A worked rubric with verification named per dimension
- Choosing the scale
- Tie-breaking
- A positive and a negative scoring pair

## Consuming the allocation arithmetic without re-deriving it

`samber/dev-event-organizer-skills@hackathon-judging` owns this arithmetic in general form. Its judge-allocation reference gives the full-coverage ceiling, the coverage-per-team formula, the three allocation bands and the feasibility gate, each derived and worked. Open it, use it, and do not rebuild it here or in a deliverable: a second copy is a second thing to keep correct.

Two adaptations, and only two:

1. **The per-company input means something different.** That skill's figure is dominated by setup and swap at a physical demo handoff, because a team plugs in and unplugs. A pitch contest runs serially off one stage with nothing to plug in, so its per-company figure is the published slot plus whatever question time the format menu allowed, plus the walk-on and walk-off.
   - Measure it against your own stage. Do not carry the hackathon figure across.
2. **The panel usually cannot be split.** That skill's stage-pitch worked example already notes that at a stage pitch the whole panel sits together and companies present serially, so the panel advances as one unit. A pitch contest is that case by default.
   - The practical consequence is that the sampled-panel rung is usually unavailable to you. The levers that remain are the ones that skill names: cut the field before the block, shorten the slot, extend the block, or move part of the judging out of the live window.
   - The pre-scored rung on this skill's format menu is the corresponding option.

One established program confirms the two-part shape without fixing its length: TechCrunch's Startup Battlefield structures a slot as a company pitch followed by live judge questions, the same shape the format menu's default rung assumes. It does not publish the minutes; measure your own stage per the instruction above.

Run its feasibility gate before fixing the slate size, not after. A slate that does not fit the block is the failure this arithmetic exists to catch, and it is caught for free before anyone is accepted.

## The observable / claimed split

This is the split the whole rubric turns on, and it is the direct consequence of there being no build phase. Sort every candidate dimension into one of two lists before assigning a single weight.

Published pitch-competition rubrics weight several dimensions already: University of Delaware's Horn Entrepreneurship judging guide weights feasibility and growth potential alongside a "wow factor" score. They confirm that weighted, multi-dimension rubrics are real practice. None makes the observable-versus-claimed split explicit, and that distinction is this skill's own construction.

**Observable in the block** - a judge sees it happen and can score it from the room.

- Whether the problem is stated in a form a stranger understands.
- Whether the product was shown working, live, on request.
- How the founder answers a question they did not prepare for.
- Whether the ask at the end is specific and matches the story that preceded it.

**Claimed, and only verifiable against the application** - a judge cannot see it and must not pretend to.

- Traction of any kind: customers, revenue, usage, retention.
- Market size and how it was arrived at.
- Team history and prior work.
- Anything about cost, margin or runway.

The rule that follows: **a claimed dimension may carry weight only if the application collected something to check it against, and the reviewer actually checked.** Otherwise the dimension scores the confidence with which a number was said aloud, which rewards exactly the wrong founder. `samber/dev-event-organizer-skills@hackathon-judging` gives the same instruction, to check that the top-weighted criterion is one a judge can observe in the slot. Applied to a startup contest, it deletes more of the rubric than it does there.

## A worked rubric with verification named per dimension

This rubric is an invented illustration built to show the mechanic. It is not a standard or a template copied from real practice; the split between weights is the organizer's own decision. Weights are written as `[w]` deliberately, since any number here would be read as a recommendation rather than a decision point.

| Dimension                      | Weight | Kind       | Where the score comes from                                                     |
| ------------------------------ | ------ | ---------- | ------------------------------------------------------------------------------ |
| Problem and product legibility | `[w]`  | Observable | The block. Did a stranger understand it                                        |
| Working product shown          | `[w]`  | Observable | The block. Shown live on request, not a recording                              |
| Answers under question         | `[w]`  | Observable | The block. Requires the format rung that has questions                         |
| Evidence of traction           | `[w]`  | Claimed    | The application's traction field, checked by a named reviewer before the block |
| Team fit to this problem       | `[w]`  | Claimed    | The application's team field, checked before the block                         |
| Fit to the award on offer      | `[w]`  | Claimed    | The application's "what the company wants" field                               |

Weights are integers summing to 100, published in the call. `samber/dev-event-organizer-skills@hackathon-judging`'s rubric reference already prescribes that shape: nobody reading a rubric should have to parse a decimal. Two properties of it are worth keeping:

- Every claimed dimension names the field it is checked against. A claimed dimension with no field is a rubric bug, not a scoring difficulty.
- The last dimension is unusual and is deliberate: an award is only worth what it is worth to the winner, and a company that does not need what is on offer will not use it. Scoring fit is how the contest avoids awarding introductions to a company that has already raised.

Keep the dimension count small. `samber/dev-event-organizer-skills@hackathon-judging`'s rubric reference argues that past a handful, the marginal criterion is scored from the same overall impression as the previous ones and adds noise rather than resolution. That argument transfers unchanged; what does not transfer is its source's own count, which was set for a talk-review rubric and is not a target here.

## Choosing the scale

An integer scale per dimension, short enough that a judge holds it in their head while a founder is still talking, and written with what the top and bottom of it mean. That is the shape `samber/dev-event-organizer-skills@hackathon-judging` argues for from the same constraint - a judge scoring live, between presenters, with no arithmetic available. What differs in a serial pitch block: judges sit through the full block, which makes drift across the running order the specific risk here rather than inconsistency across a field of simultaneous tables.

Two consequences of the serial block:

- **Score after each company, before the next starts.** A panel scoring at the end scores a memory, and the memory favours whoever went last.
- **Say in the brief that the first company is scored against the scale, not against nothing.** A panel still calibrating disadvantages the first slot, and naming it is the cheapest correction available.

A peer-reviewed rating instrument built specifically to measure entrepreneurial pitch success (the Entrepreneurial Pitching Success scale, published in the Journal of Small Business Management) tested the anchoring instruction above directly, by measuring judge agreement rather than assuming it. An unanchored five-point scale scored by untrained coders produced weak agreement on most items, measured by intraclass correlation coefficient. Agreement improved once the scale carried written behavioral anchors and a third coder was added, though it never became uniformly strong.

The same study found a second effect worth planning for rather than mistaking for a rubric defect. When everyone on stage is well-rehearsed, the range of scores narrows and measured agreement drops for a reason that has nothing to do with the rubric: a professionally coached field really does look more alike than a mixed one. Do not read a tight, low-variance spread on one dimension as proof the anchors failed; check whether the field itself was uniformly polished before rewriting the scale.

## Tie-breaking

The ladder below handles ties in pitch contests. Publish it in the call before applications close rather than inventing one at the podium.

1. Highest score on the highest-weighted **observable** dimension. The observable dimensions are the ones every judge saw under identical conditions, which makes them the fairest thing to reach for first.
2. Highest count of judges who placed the company first. This is not this skill's own invention: a major university-run business-plan competition, whose judges rank companies rather than score them, publishes exactly this fallback. A tied aggregate ranking there resolves to whichever company collected the most first-place rankings.
3. A named tie-break judge decides, on the record, in one sentence; that judge must be one with no declared interest in either company. This is the point where the conflict rule and the tie-break ladder meet, and a panel that never wrote its interests down cannot satisfy it.
4. A shared award, only where every item in it is divisible and the call said so in advance. Introductions divide; a single program place does not.

Do not break a tie by re-pitching, and do not break it by a random draw. The first re-scores a performance that already happened under different conditions; the second is the mechanic that can move a prize contest across a contest-law line, which is one of the categories this skill routes to counsel rather than answering.

## A positive and a negative scoring pair

Both sheets below are illustrations. Every score in them is invented to show the mechanic - none is a threshold or a real judge's sheet.

**Negative - what a judge writes on a sheet with no dimensions**

> Company 3: 8/10. Great team, big market, I'd take a meeting.

Nothing survives a challenge. It cannot say which dimension carried the score, cannot be compared with another judge's 8, cannot feed a tie-break. The specific failure here: "I'd take a meeting" is the judge's own interest showing up inside the score, which is precisely what the conflict rule is meant to keep visible and separate.

**Positive - the same judge, same block, on the rubric above**

> Company 3 - Legibility 5 (a stranger could repeat the problem back). Working product 4 (shown live, one path stubbed and they said so). Answers under question 5 (took the churn question head-on). Traction 3 (application lists customers with dates; two are pilots, not paid). Team fit 4. Award fit 2 (already raised; introductions are not what they need). Declared interest: none.

Every score names its evidence in a clause, the claimed dimensions point back at the application rather than at the stage, the award-fit score is doing real work, and the interest line is present and empty. That is what makes a non-empty one unremarkable when it appears.
