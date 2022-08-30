package main

func generateTemplate(state string) Message {
	tmpl := map[string]Message{
		"created": Message{
			Subject: "New shift added: %s",
			// Text template for new shifts
			TextBody: `Greetings,

New shift added for '%s' starting on %s from %s.

https://m.shiftboard.com/onlocationexp/schedules/shifts/%s

Thank you,
ShiftBoard Bot`,
			// HTML template for new shifts
			HtmlBody: `Greetings,
<p>
New shift added for <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts/%s'>%s</a> starting on <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts'>%s from %s</a>.
</p>
<p>
Thank you,<br>
ShiftBoard Bot
</p>`,
		},
		"updated": Message{
			Subject: "Shift updated: %s",
			// Text template for updated shifts
			TextBody: `Greetings,

The '%s' shift has been updated. The current start date and time is %s from %s.

https://m.shiftboard.com/onlocationexp/schedules/shifts/%s\n

Thank you,
ShiftBoard Bot`,
			// HTML template for updated shifts
			HtmlBody: `Greetings,
<p>
The <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts/%s'>%s</a> shift has been updated. The current start date is <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts'>%s from %s</a>.
</p>
<p>
Thank you,<br>
ShiftBoard Bot
</p>`,
		},
	}

	return tmpl[state]
}
