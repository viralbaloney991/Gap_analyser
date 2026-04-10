Goal : 

1.) Find Gaps that is what is currently not being tracked as per MITRE Framework
2.) redundant detections i.e. overlapping detections 


Setup :
**NOTE** _Please use production grade coding practices here including but not limited to : masking API key supplied by the enduser on frontend,segrating frontend and backend,factoring code into clear different paths etc._

1.)User selects region and provides API key. The user then either clicks on one of the two buttons: 
	a.) Get MITRE Coverage (functionality defined in 6.c)
	b.) Get Alert insights (functionality defined in 6.d)
2.)Endpoints are defined in the route for various regions e.g. us1 ,us2, eu1 etc.
3.)Documentation for grpc calls is defined under : https://coralogix.com/docs/developer-portal/apis/data-management/alerts-api/alerts-grpc-api/#request-structure-summary

4.)Only allowed Api calls : 
Method:	Permission
ListAlertDefs:alerts:ReadConfig OR logs.alerts:ReadConfig OR metrics.alerts:ReadConfig OR spans.alerts:ReadConfig
GetAlertDef:alerts:ReadConfig OR logs.alerts:ReadConfig OR metrics.alerts:ReadConfig OR spans.alerts:ReadConfig

5.) Confirm data retrieved in JSON. if not then make it in JSON.
6.) Once you have the data :
	a.) Filter all active alerts
	b.) Now Extract features Key dimensions:
			Data source (Okta, AWS, M365)
			Entity (user, IP, device)
			Action (login, API call)
			Conditions (geo anomaly, spike, rare event)
			Time window
			Technique (MITRE)
	c.)Create a MITRE map with Techniques/Tactics not covered highlighted in red/orange/yellow/green:
			Red : Not covered at all
			Orange : Covered upto 50% 
			Yellow : covered upto 75 %
			Green : Covered 100%
		For this attack navigator can be utilised. Public repo : https://github.com/mitre-attack/attack-navigator/

	d.)Develop a Similarity Engine that compares detections:
		Use:Simple scoring (v1) or embeddings / vector similarity 
		Group detections into “families”:
		Example:
		All “Impossible Travel” detections
		All “Privilege Escalation” detections

		Example output:
		1.Detection A vs B → 85% similar. Keep A as A is a super set of B , however B is specific to a xyz parameter and could be a custom requirement
		2.Detection C → unique 
		3. Duplicate detection
		“These 4 detections are functionally identical”
		4. Merge suggestion
		“You can replace 5 rules with 1 generalized rule”
		5. Coverage insight
		“You have 10 detections for login anomalies but none for token abuse”
		6. Tuning propagation
		“This suppression logic can be applied to 6 similar detections”
	e.)For demo this can run locally; but later can be hosted on Cloud to be accessed by all other members so keep this in mind.
	 
