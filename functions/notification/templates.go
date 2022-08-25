package main

func generateTemplate(state string) Message {
	tmpl := map[string]Message{
		"created": Message{
			Subject: "New shift added: %s",
			TextBody: `Greetings,

New shift added for '%s' starting on %s.

https://m.shiftboard.com/onlocationexp/schedules/shifts/%s

Thank you,
ShiftBoard Bot`,
			HtmlBody: `Greetings,
<p>
New shift added for <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts/%s'>%s</a> starting on <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts'>%s</a>.
</p>
<p>
Thank you,<br>
ShiftBoard Bot
</p>`,
		},
		"updated": Message{
			Subject: "Shift updated: %s",
			TextBody: `Greetings,

Shift for '%s' has been updated. The current start date is %s.

https://m.shiftboard.com/onlocationexp/schedules/shifts/%s\n

Thank you,
ShiftBoard Bot`,
			HtmlBody: `Greetings,
<p>
Shift for <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts/%s'>%s</a> has been updated. The current start date is <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts'>%s</a>.
</p>
<p>
Thank you,<br>
ShiftBoard Bot
</p>`,
		},
	}

	return tmpl[state]
}
