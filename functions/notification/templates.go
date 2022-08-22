package main

func generateTemplate(state string) Message {
	tmpl := map[string]Message{
		"created": Message{
			Subject: "New shift added: %s",
			TextBody: `Greetings,

Shift has been added for '%s' on %s.

https://m.shiftboard.com/onlocationexp/schedules/shifts/%s

Thank you,
ShiftBoard Bot`,
			HtmlBody: `Greetings,
<p>
Shift has been added for <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts/%s'>%s</a> on <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts'>%s</a>.
</p>
<p>
Thank you,<br>
ShiftBoard Bot
</p>`,
		},
		"updated": Message{
			Subject: "Shift updated: %s",
			TextBody: `Greetings,

Shift for '%s' was updated on %s.

https://m.shiftboard.com/onlocationexp/schedules/shifts/%s\n

Thank you,
ShiftBoard Bot`,
			HtmlBody: `Greetings,
<p>
Shift for <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts/%s'>%s</a> was updated on <a href='https://m.shiftboard.com/onlocationexp/schedules/shifts'>%s</a>.
</p>
<p>
Thank you,<br>
ShiftBoard Bot
</p>`,
		},
	}

	return tmpl[state]
}
