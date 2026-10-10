# Payout exposure categories and where to route them

## The standing policy

**This skill states no numeric threshold, no tax-form name, and no currency amount. Ever.**

Three reasons, and they compound:

1. Contest-law classification, tax reporting and withholding, cross-border payment rules and the treatment of minors are all jurisdiction-specific.
2. All of them change over time, and a skill file does not.
3. A reader following a document about handing money to strangers has more reason than usual to act on a specific-looking figure without checking it. A fabricated threshold here converts into a real obligation missed.

The correct output shape is always the same: name the category, say the specific answer is jurisdiction-specific and current only where it was checked, and name who to ask.

## The six categories

**1. Contest-law classification.** Whether a prize competition counts as a skill-based contest or a game of chance in a given jurisdiction, which changes the regulatory regime applied to it. A tie broken by a random draw is one of the few organizer decisions that could move an event across that line. `samber/dev-event-organizer-skills@hackathon-judging` avoids a coin-flip tie-break for this reason and states that it could not verify the constraint against a legal source, so route the question rather than expecting either skill to answer it.

**2. Payer-side tax reporting and withholding.** An award with monetary value can create a reporting or withholding obligation, and it typically falls on the payer, the organizing entity, rather than on the winner. MLH's own legal checklist names the category without answering it: "Are there any tax requirements we should be aware of? Do we need to keep our receipts?"

**3. Cross-border payment.** Two separate problems wear one name: whether the payment rail can reach the winner at all, and what rules and treaty questions apply to a payment leaving the organizer's jurisdiction. The organizer's bank or platform answers the first. Nobody there answers the second.

**4. Minors receiving an award.** A winner under the age of majority may need a parent or guardian to receive or countersign. MLH's checklist covers a different question, a waiver for a high-school-age participant to stay at a venue overnight, which is a venue and safeguarding matter rather than a payout one. The two get conflated because they surface from the same fact.

**5. Sponsor employees receiving that sponsor's own track prize.** Distinct from the judging conflict of interest that `samber/dev-event-organizer-skills@hackathon-judging` already handles for the rank. That skill keeps a sponsor's representative from deciding their own track's outcome.

This category is the separate question of whether that sponsor's employee may take home the award, which survives even where the sponsor had no say in the rank at all. Answer it in the sponsorship agreement, before the event.

**6. Organizers, volunteers, mentors and judges receiving anything.** The one category with published practice - see the eligibility reference. Adopt it, do not re-derive it.

## The routing ladder

Ordered by how far the question has already travelled, cheapest first:

1. **Answer it from published practice** where published practice exists. Only category 6 qualifies.
2. **Route to the fiscal host or the institution holding the money.** They have already answered these questions for every event they host, and asking them costs one message.
3. **Route to the funding sponsor's own legal team** for anything specific to their prize - their employees' eligibility, their credits' terms, their preferred payout process.
4. **Route to counsel.** The right answer whenever categories 1-5 are live and no host or sponsor can absorb them, and the only answer when a prize pool is large enough that a losing team might retain counsel of their own.

Never substitute a plausible-sounding sentence for any rung of this ladder. "We looked into it and it should be fine" is the failure this whole reference exists to prevent.

## The fiscal-host pattern

An organizing team with no legal entity of its own has a standard route. MLH names it: work with a fiscal sponsor, an existing tax-exempt nonprofit, and be granted its tax-exempt status along with its financial and legal infrastructure. MLH names Hack Club and Hack+ as hackathon-specific options, and states that a team which is not a student organization should consult MLH itself to evaluate legal alternatives.

Beyond the tax status, the same checklist names the operational questions that decide whether a prize can be paid on time at all. Ask them of any host:

- Who has signing authority for agreements on the organization's behalf, and how long does their review take?
- Where are the funds held, how are they accessed, and what fees apply?
- What is the realistic timeline between money arriving in the account and being spendable?

That last one is the sleeper. A prize funded by a sponsorship that clears after the event is a prize the organizer pays personally in the meantime.

## What a written payout plan contains

Six lines, each answering a question that otherwise gets improvised at the podium. Write them before the brief is published, not after the ceremony.

1. **Medium** - what the award is, and for a credit or voucher its expiry, region and account requirement.
2. **Handover** - who physically hands over or transfers each award.
3. **Claim** - what the winner must provide, described as a category rather than as a named document.
4. **Finality** - when the award becomes final, where any continuing condition applies.
5. **Lapse** - what happens to an unclaimed award, and by when.
6. **Routing** - which exposure categories were live, who each was routed to, and the answer where one came back.
