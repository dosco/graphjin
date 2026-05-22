// Code generated from SnowflakeParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package snowflake // SnowflakeParser
import "github.com/antlr4-go/antlr/v4"

type BaseSnowflakeParserVisitor struct {
	*antlr.BaseParseTreeVisitor
}

func (v *BaseSnowflakeParserVisitor) VisitSnowflake_file(ctx *Snowflake_fileContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitBatch(ctx *BatchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSql_command(ctx *Sql_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDdl_command(ctx *Ddl_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDml_command(ctx *Dml_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitInsert_statement(ctx *Insert_statementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitInsert_multi_table_statement(ctx *Insert_multi_table_statementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitInto_clause2(ctx *Into_clause2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitValues_list(ctx *Values_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitValue_item(ctx *Value_itemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitMerge_statement(ctx *Merge_statementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitMerge_matches(ctx *Merge_matchesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitMerge_update_delete(ctx *Merge_update_deleteContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitMerge_insert(ctx *Merge_insertContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUpdate_statement(ctx *Update_statementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTable_or_query(ctx *Table_or_queryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDelete_statement(ctx *Delete_statementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitValues_builder(ctx *Values_builderContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitOther_command(ctx *Other_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCopy_into_table(ctx *Copy_into_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExternal_location(ctx *External_locationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFiles(ctx *FilesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFile_format(ctx *File_formatContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFormat_name(ctx *Format_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFormat_type(ctx *Format_typeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitStage_file_format(ctx *Stage_file_formatContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCopy_into_location(ctx *Copy_into_locationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitComment(ctx *CommentContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCommit(ctx *CommitContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExecute_immediate(ctx *Execute_immediateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExecute_task(ctx *Execute_taskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExplain(ctx *ExplainContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitParallel(ctx *ParallelContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitGet_dml(ctx *Get_dmlContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitGrant_ownership(ctx *Grant_ownershipContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitGrant_to_role(ctx *Grant_to_roleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitGlobal_privileges(ctx *Global_privilegesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitGlobal_privilege(ctx *Global_privilegeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAccount_object_privileges(ctx *Account_object_privilegesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAccount_object_privilege(ctx *Account_object_privilegeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSchema_privileges(ctx *Schema_privilegesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSchema_privilege(ctx *Schema_privilegeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSchema_object_privileges(ctx *Schema_object_privilegesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSchema_object_privilege(ctx *Schema_object_privilegeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitGrant_to_share(ctx *Grant_to_shareContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitObject_privilege(ctx *Object_privilegeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitGrant_role(ctx *Grant_roleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRole_name(ctx *Role_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSystem_defined_role(ctx *System_defined_roleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitList(ctx *ListContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitInternal_stage(ctx *Internal_stageContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExternal_stage(ctx *External_stageContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitPut(ctx *PutContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRemove(ctx *RemoveContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRevoke_from_role(ctx *Revoke_from_roleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRevoke_from_share(ctx *Revoke_from_shareContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRevoke_role(ctx *Revoke_roleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRollback(ctx *RollbackContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSet(ctx *SetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTruncate_materialized_view(ctx *Truncate_materialized_viewContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTruncate_table(ctx *Truncate_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUnset(ctx *UnsetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_command(ctx *Alter_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAccount_params(ctx *Account_paramsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitObject_params(ctx *Object_paramsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDefault_ddl_collation(ctx *Default_ddl_collationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitObject_properties(ctx *Object_propertiesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSession_params(ctx *Session_paramsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_account(ctx *Alter_accountContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitEnabled_true_false(ctx *Enabled_true_falseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_alert(ctx *Alter_alertContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitResume_suspend(ctx *Resume_suspendContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlert_set_clause(ctx *Alert_set_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlert_unset_clause(ctx *Alert_unset_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_api_integration(ctx *Alter_api_integrationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitApi_integration_property(ctx *Api_integration_propertyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_connection(ctx *Alter_connectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_database(ctx *Alter_databaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDatabase_property(ctx *Database_propertyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAccount_id_list(ctx *Account_id_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_dynamic_table(ctx *Alter_dynamic_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_external_table(ctx *Alter_external_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitIgnore_edition_check(ctx *Ignore_edition_checkContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitReplication_schedule(ctx *Replication_scheduleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDb_name_list(ctx *Db_name_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShare_name_list(ctx *Share_name_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFull_acct_list(ctx *Full_acct_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_failover_group(ctx *Alter_failover_groupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_file_format(ctx *Alter_file_formatContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_function(ctx *Alter_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_function_signature(ctx *Alter_function_signatureContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitData_type_list(ctx *Data_type_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_masking_policy(ctx *Alter_masking_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_materialized_view(ctx *Alter_materialized_viewContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_network_policy(ctx *Alter_network_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_notification_integration(ctx *Alter_notification_integrationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_pipe(ctx *Alter_pipeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_procedure(ctx *Alter_procedureContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_replication_group(ctx *Alter_replication_groupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCredit_quota(ctx *Credit_quotaContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFrequency(ctx *FrequencyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitNotify_users(ctx *Notify_usersContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTriggerDefinition(ctx *TriggerDefinitionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_resource_monitor(ctx *Alter_resource_monitorContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_role(ctx *Alter_roleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_row_access_policy(ctx *Alter_row_access_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_schema(ctx *Alter_schemaContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSchema_property(ctx *Schema_propertyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_security_integration(ctx *Alter_security_integrationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_security_integration_external_oauth(ctx *Alter_security_integration_external_oauthContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSecurity_integration_external_oauth_property(ctx *Security_integration_external_oauth_propertyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_security_integration_snowflake_oauth(ctx *Alter_security_integration_snowflake_oauthContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSecurity_integration_snowflake_oauth_property(ctx *Security_integration_snowflake_oauth_propertyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_security_integration_saml2(ctx *Alter_security_integration_saml2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_security_integration_scim(ctx *Alter_security_integration_scimContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSecurity_integration_scim_property(ctx *Security_integration_scim_propertyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_sequence(ctx *Alter_sequenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_session(ctx *Alter_sessionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_session_policy(ctx *Alter_session_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_share(ctx *Alter_shareContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_stage(ctx *Alter_stageContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_storage_integration(ctx *Alter_storage_integrationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_stream(ctx *Alter_streamContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_table(ctx *Alter_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitClustering_action(ctx *Clustering_actionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTable_column_action(ctx *Table_column_actionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitInline_constraint(ctx *Inline_constraintContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitConstraint_properties(ctx *Constraint_propertiesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExt_table_column_action(ctx *Ext_table_column_actionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitConstraint_action(ctx *Constraint_actionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSearch_optimization_action(ctx *Search_optimization_actionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSearch_method_with_target(ctx *Search_method_with_targetContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_table_alter_column(ctx *Alter_table_alter_columnContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_column_decl_list(ctx *Alter_column_decl_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_column_decl(ctx *Alter_column_declContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_column_opts(ctx *Alter_column_optsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitColumn_set_tags(ctx *Column_set_tagsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitColumn_unset_tags(ctx *Column_unset_tagsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_tag(ctx *Alter_tagContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_task(ctx *Alter_taskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_user(ctx *Alter_userContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_view(ctx *Alter_viewContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_modify(ctx *Alter_modifyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_warehouse(ctx *Alter_warehouseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_connection_opts(ctx *Alter_connection_optsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_user_opts(ctx *Alter_user_optsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_tag_opts(ctx *Alter_tag_optsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_network_policy_opts(ctx *Alter_network_policy_optsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_warehouse_opts(ctx *Alter_warehouse_optsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlter_account_opts(ctx *Alter_account_optsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSet_tags(ctx *Set_tagsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTag_decl_list(ctx *Tag_decl_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUnset_tags(ctx *Unset_tagsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_command(ctx *Create_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_account(ctx *Create_accountContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_alert(ctx *Create_alertContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlert_condition(ctx *Alert_conditionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlert_action(ctx *Alert_actionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_api_integration(ctx *Create_api_integrationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_object_clone(ctx *Create_object_cloneContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_connection(ctx *Create_connectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_database(ctx *Create_databaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_dynamic_table(ctx *Create_dynamic_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitClone_at_before(ctx *Clone_at_beforeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAt_before1(ctx *At_before1Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitHeader_decl(ctx *Header_declContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCompression_type(ctx *Compression_typeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCompression(ctx *CompressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_external_function(ctx *Create_external_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_external_table(ctx *Create_external_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExternal_table_column_decl(ctx *External_table_column_declContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExternal_table_column_decl_list(ctx *External_table_column_decl_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFull_acct(ctx *Full_acctContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitIntegration_type_name(ctx *Integration_type_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_failover_group(ctx *Create_failover_groupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitType_fileformat(ctx *Type_fileformatContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_file_format(ctx *Create_file_formatContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitArg_decl(ctx *Arg_declContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCol_decl(ctx *Col_declContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFunction_definition(ctx *Function_definitionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_function(ctx *Create_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_managed_account(ctx *Create_managed_accountContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_masking_policy(ctx *Create_masking_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTag_decl(ctx *Tag_declContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitColumn_list_in_parentheses(ctx *Column_list_in_parenthesesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_materialized_view(ctx *Create_materialized_viewContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_network_policy(ctx *Create_network_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCloud_provider_params_auto(ctx *Cloud_provider_params_autoContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCloud_provider_params_push(ctx *Cloud_provider_params_pushContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_notification_integration(ctx *Create_notification_integrationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_pipe(ctx *Create_pipeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCaller_owner(ctx *Caller_ownerContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExecuta_as(ctx *Executa_asContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitProcedure_definition(ctx *Procedure_definitionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_procedure(ctx *Create_procedureContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_replication_group(ctx *Create_replication_groupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_resource_monitor(ctx *Create_resource_monitorContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_role(ctx *Create_roleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_row_access_policy(ctx *Create_row_access_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_schema(ctx *Create_schemaContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_security_integration_external_oauth(ctx *Create_security_integration_external_oauthContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitImplicit_none(ctx *Implicit_noneContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_security_integration_snowflake_oauth(ctx *Create_security_integration_snowflake_oauthContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_security_integration_saml2(ctx *Create_security_integration_saml2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_security_integration_scim(ctx *Create_security_integration_scimContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitNetwork_policy(ctx *Network_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitPartner_application(ctx *Partner_applicationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitStart_with(ctx *Start_withContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitIncrement_by(ctx *Increment_byContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_sequence(ctx *Create_sequenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_session_policy(ctx *Create_session_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_share(ctx *Create_shareContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCharacter(ctx *CharacterContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFormat_type_options(ctx *Format_type_optionsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCopy_options(ctx *Copy_optionsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitInternal_stage_params(ctx *Internal_stage_paramsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitStage_type(ctx *Stage_typeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitStage_master_key(ctx *Stage_master_keyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitStage_kms_key(ctx *Stage_kms_keyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitStage_encryption_opts_aws(ctx *Stage_encryption_opts_awsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAws_token(ctx *Aws_tokenContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAws_key_id(ctx *Aws_key_idContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAws_secret_key(ctx *Aws_secret_keyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAws_role(ctx *Aws_roleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExternal_stage_params(ctx *External_stage_paramsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTrue_false(ctx *True_falseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitEnable(ctx *EnableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRefresh_on_create(ctx *Refresh_on_createContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAuto_refresh(ctx *Auto_refreshContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitNotification_integration(ctx *Notification_integrationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDirectory_table_params(ctx *Directory_table_paramsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_stage(ctx *Create_stageContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCloud_provider_params(ctx *Cloud_provider_paramsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCloud_provider_params2(ctx *Cloud_provider_params2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCloud_provider_params3(ctx *Cloud_provider_params3Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_storage_integration(ctx *Create_storage_integrationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCopy_grants(ctx *Copy_grantsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAppend_only(ctx *Append_onlyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitInsert_only(ctx *Insert_onlyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_initial_rows(ctx *Show_initial_rowsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitStream_time(ctx *Stream_timeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_stream(ctx *Create_streamContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTemporary(ctx *TemporaryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTable_type(ctx *Table_typeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitWith_tags(ctx *With_tagsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitWith_row_access_policy(ctx *With_row_access_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCluster_by(ctx *Cluster_byContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitChange_tracking(ctx *Change_trackingContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitWith_masking_policy(ctx *With_masking_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCollate(ctx *CollateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitNot_null(ctx *Not_nullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDefault_value(ctx *Default_valueContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitForeign_key(ctx *Foreign_keyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitOut_of_line_constraint(ctx *Out_of_line_constraintContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFull_col_decl(ctx *Full_col_declContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitColumn_decl_item(ctx *Column_decl_itemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitColumn_decl_item_list(ctx *Column_decl_item_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_table(ctx *Create_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_table_as_select(ctx *Create_table_as_selectContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_tag(ctx *Create_tagContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSession_parameter(ctx *Session_parameterContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSession_parameter_list(ctx *Session_parameter_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSession_parameter_init_list(ctx *Session_parameter_init_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSession_parameter_init(ctx *Session_parameter_initContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_task(ctx *Create_taskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSql(ctx *SqlContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCall(ctx *CallContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_user(ctx *Create_userContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitView_col(ctx *View_colContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_view(ctx *Create_viewContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCreate_warehouse(ctx *Create_warehouseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitWh_properties(ctx *Wh_propertiesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitWh_params(ctx *Wh_paramsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTrigger_definition(ctx *Trigger_definitionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitObject_type_name(ctx *Object_type_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitObject_type_plural(ctx *Object_type_pluralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_command(ctx *Drop_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_object(ctx *Drop_objectContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_alert(ctx *Drop_alertContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_connection(ctx *Drop_connectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_database(ctx *Drop_databaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_dynamic_table(ctx *Drop_dynamic_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_external_table(ctx *Drop_external_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_failover_group(ctx *Drop_failover_groupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_file_format(ctx *Drop_file_formatContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_function(ctx *Drop_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_integration(ctx *Drop_integrationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_managed_account(ctx *Drop_managed_accountContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_masking_policy(ctx *Drop_masking_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_materialized_view(ctx *Drop_materialized_viewContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_network_policy(ctx *Drop_network_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_pipe(ctx *Drop_pipeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_procedure(ctx *Drop_procedureContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_replication_group(ctx *Drop_replication_groupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_resource_monitor(ctx *Drop_resource_monitorContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_role(ctx *Drop_roleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_row_access_policy(ctx *Drop_row_access_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_schema(ctx *Drop_schemaContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_sequence(ctx *Drop_sequenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_session_policy(ctx *Drop_session_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_share(ctx *Drop_shareContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_stage(ctx *Drop_stageContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_stream(ctx *Drop_streamContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_table(ctx *Drop_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_tag(ctx *Drop_tagContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_task(ctx *Drop_taskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_user(ctx *Drop_userContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_view(ctx *Drop_viewContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDrop_warehouse(ctx *Drop_warehouseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCascade_restrict(ctx *Cascade_restrictContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitArg_types(ctx *Arg_typesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUndrop_command(ctx *Undrop_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUndrop_database(ctx *Undrop_databaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUndrop_dynamic_table(ctx *Undrop_dynamic_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUndrop_schema(ctx *Undrop_schemaContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUndrop_table(ctx *Undrop_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUndrop_tag(ctx *Undrop_tagContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUse_command(ctx *Use_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUse_database(ctx *Use_databaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUse_role(ctx *Use_roleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUse_schema(ctx *Use_schemaContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUse_secondary_roles(ctx *Use_secondary_rolesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitUse_warehouse(ctx *Use_warehouseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitComment_clause(ctx *Comment_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitIf_suspended(ctx *If_suspendedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitIf_exists(ctx *If_existsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitIf_not_exists(ctx *If_not_existsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitOr_replace(ctx *Or_replaceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe(ctx *DescribeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_command(ctx *Describe_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_alert(ctx *Describe_alertContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_database(ctx *Describe_databaseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_dynamic_table(ctx *Describe_dynamic_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_external_table(ctx *Describe_external_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_file_format(ctx *Describe_file_formatContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_function(ctx *Describe_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_integration(ctx *Describe_integrationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_masking_policy(ctx *Describe_masking_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_materialized_view(ctx *Describe_materialized_viewContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_network_policy(ctx *Describe_network_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_pipe(ctx *Describe_pipeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_procedure(ctx *Describe_procedureContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_result(ctx *Describe_resultContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_row_access_policy(ctx *Describe_row_access_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_schema(ctx *Describe_schemaContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_search_optimization(ctx *Describe_search_optimizationContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_sequence(ctx *Describe_sequenceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_session_policy(ctx *Describe_session_policyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_share(ctx *Describe_shareContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_stage(ctx *Describe_stageContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_stream(ctx *Describe_streamContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_table(ctx *Describe_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_task(ctx *Describe_taskContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_transaction(ctx *Describe_transactionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_user(ctx *Describe_userContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_view(ctx *Describe_viewContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDescribe_warehouse(ctx *Describe_warehouseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_command(ctx *Show_commandContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_alerts(ctx *Show_alertsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_columns(ctx *Show_columnsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_connections(ctx *Show_connectionsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitStarts_with(ctx *Starts_withContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitLimit_rows(ctx *Limit_rowsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_databases(ctx *Show_databasesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_databases_in_failover_group(ctx *Show_databases_in_failover_groupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_databases_in_replication_group(ctx *Show_databases_in_replication_groupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_delegated_authorizations(ctx *Show_delegated_authorizationsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_external_functions(ctx *Show_external_functionsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_dynamic_tables(ctx *Show_dynamic_tablesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_external_tables(ctx *Show_external_tablesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_failover_groups(ctx *Show_failover_groupsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_file_formats(ctx *Show_file_formatsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_functions(ctx *Show_functionsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_global_accounts(ctx *Show_global_accountsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_grants(ctx *Show_grantsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_grants_opts(ctx *Show_grants_optsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_integrations(ctx *Show_integrationsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_locks(ctx *Show_locksContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_managed_accounts(ctx *Show_managed_accountsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_masking_policies(ctx *Show_masking_policiesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitIn_obj(ctx *In_objContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitIn_obj_2(ctx *In_obj_2Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_materialized_views(ctx *Show_materialized_viewsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_network_policies(ctx *Show_network_policiesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_objects(ctx *Show_objectsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_organization_accounts(ctx *Show_organization_accountsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitIn_for(ctx *In_forContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_parameters(ctx *Show_parametersContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_pipes(ctx *Show_pipesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_primary_keys(ctx *Show_primary_keysContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_procedures(ctx *Show_proceduresContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_regions(ctx *Show_regionsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_replication_accounts(ctx *Show_replication_accountsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_replication_databases(ctx *Show_replication_databasesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_replication_groups(ctx *Show_replication_groupsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_resource_monitors(ctx *Show_resource_monitorsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_roles(ctx *Show_rolesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_row_access_policies(ctx *Show_row_access_policiesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_schemas(ctx *Show_schemasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_sequences(ctx *Show_sequencesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_session_policies(ctx *Show_session_policiesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_shares(ctx *Show_sharesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_shares_in_failover_group(ctx *Show_shares_in_failover_groupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_shares_in_replication_group(ctx *Show_shares_in_replication_groupContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_stages(ctx *Show_stagesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_streams(ctx *Show_streamsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_tables(ctx *Show_tablesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_tags(ctx *Show_tagsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_tasks(ctx *Show_tasksContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_transactions(ctx *Show_transactionsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_user_functions(ctx *Show_user_functionsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_users(ctx *Show_usersContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_variables(ctx *Show_variablesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_views(ctx *Show_viewsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitShow_warehouses(ctx *Show_warehousesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitLike_pattern(ctx *Like_patternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAccount_identifier(ctx *Account_identifierContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSchema_name(ctx *Schema_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitObject_type(ctx *Object_typeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitObject_type_list(ctx *Object_type_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTag_value(ctx *Tag_valueContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitArg_data_type(ctx *Arg_data_typeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitArg_name(ctx *Arg_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitParam_name(ctx *Param_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRegion_group_id(ctx *Region_group_idContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSnowflake_region_id(ctx *Snowflake_region_idContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitString(ctx *StringContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitString_list(ctx *String_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitId_(ctx *Id_Context) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitKeyword(ctx *KeywordContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitNon_reserved_words(ctx *Non_reserved_wordsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitBuiltin_function(ctx *Builtin_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitList_operator(ctx *List_operatorContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitBinary_builtin_function(ctx *Binary_builtin_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitBinary_or_ternary_builtin_function(ctx *Binary_or_ternary_builtin_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTernary_builtin_function(ctx *Ternary_builtin_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitPattern(ctx *PatternContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitColumn_name(ctx *Column_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitColumn_list(ctx *Column_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitObject_name(ctx *Object_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitNum(ctx *NumContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExpr_list(ctx *Expr_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExpr_list_sorted(ctx *Expr_list_sortedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExpr(ctx *ExprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitIff_expr(ctx *Iff_exprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTrim_expression(ctx *Trim_expressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTry_cast_expr(ctx *Try_cast_exprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitJson_literal(ctx *Json_literalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitKv_pair(ctx *Kv_pairContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitValue(ctx *ValueContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitArr_literal(ctx *Arr_literalContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitData_type(ctx *Data_typeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitPrimitive_expression(ctx *Primitive_expressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitOrder_by_expr(ctx *Order_by_exprContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAsc_desc(ctx *Asc_descContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitOver_clause(ctx *Over_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFunction_call(ctx *Function_callContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRanking_windowed_function(ctx *Ranking_windowed_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAggregate_function(ctx *Aggregate_functionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitLiteral(ctx *LiteralContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSign(ctx *SignContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFull_column_name(ctx *Full_column_nameContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitBracket_expression(ctx *Bracket_expressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCase_expression(ctx *Case_expressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSwitch_search_condition_section(ctx *Switch_search_condition_sectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSwitch_section(ctx *Switch_sectionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitQuery_statement(ctx *Query_statementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitWith_expression(ctx *With_expressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitCommon_table_expression(ctx *Common_table_expressionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAnchor_clause(ctx *Anchor_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRecursive_clause(ctx *Recursive_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSelect_statement(ctx *Select_statementContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSet_operators(ctx *Set_operatorsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSelect_optional_clauses(ctx *Select_optional_clausesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSelect_clause(ctx *Select_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSelect_top_clause(ctx *Select_top_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSelect_list_no_top(ctx *Select_list_no_topContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSelect_list_top(ctx *Select_list_topContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSelect_list(ctx *Select_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSelect_list_elem(ctx *Select_list_elemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitColumn_elem(ctx *Column_elemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAs_alias(ctx *As_aliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExpression_elem(ctx *Expression_elemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitColumn_position(ctx *Column_positionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAll_distinct(ctx *All_distinctContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTop_clause(ctx *Top_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitInto_clause(ctx *Into_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitVar_list(ctx *Var_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitVar(ctx *VarContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFrom_clause(ctx *From_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTable_sources(ctx *Table_sourcesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTable_source(ctx *Table_sourceContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitTable_source_item_joined(ctx *Table_source_item_joinedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitObject_ref(ctx *Object_refContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFlatten_table_option(ctx *Flatten_table_optionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFlatten_table(ctx *Flatten_tableContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitPrior_list(ctx *Prior_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitPrior_item(ctx *Prior_itemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitOuter_join(ctx *Outer_joinContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitJoin_type(ctx *Join_typeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitJoin_clause(ctx *Join_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAt_before(ctx *At_beforeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitEnd(ctx *EndContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitChanges(ctx *ChangesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDefault_append_only(ctx *Default_append_onlyContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitPartition_by(ctx *Partition_byContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAlias(ctx *AliasContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExpr_alias_list(ctx *Expr_alias_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitMeasures(ctx *MeasuresContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitMatch_opts(ctx *Match_optsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRow_match(ctx *Row_matchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFirst_last(ctx *First_lastContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSymbol(ctx *SymbolContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitAfter_match(ctx *After_matchContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSymbol_list(ctx *Symbol_listContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitDefine(ctx *DefineContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitMatch_recognize(ctx *Match_recognizeContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitPivot_unpivot(ctx *Pivot_unpivotContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitColumn_alias_list_in_brackets(ctx *Column_alias_list_in_bracketsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitExpr_list_in_parentheses(ctx *Expr_list_in_parenthesesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitValues(ctx *ValuesContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSample_method(ctx *Sample_methodContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRepeatable_seed(ctx *Repeatable_seedContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSample_opts(ctx *Sample_optsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSample(ctx *SampleContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSearch_condition(ctx *Search_conditionContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitComparison_operator(ctx *Comparison_operatorContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitNull_not_null(ctx *Null_not_nullContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSubquery(ctx *SubqueryContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitPredicate(ctx *PredicateContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitWhere_clause(ctx *Where_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitGroup_item(ctx *Group_itemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitGroup_by_clause(ctx *Group_by_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitHaving_clause(ctx *Having_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitQualify_clause(ctx *Qualify_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitOrder_item(ctx *Order_itemContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitOrder_by_clause(ctx *Order_by_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitRow_rows(ctx *Row_rowsContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitFirst_next(ctx *First_nextContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitLimit_clause(ctx *Limit_clauseContext) interface{} {
	return v.VisitChildren(ctx)
}

func (v *BaseSnowflakeParserVisitor) VisitSupplement_non_reserved_words(ctx *Supplement_non_reserved_wordsContext) interface{} {
	return v.VisitChildren(ctx)
}
