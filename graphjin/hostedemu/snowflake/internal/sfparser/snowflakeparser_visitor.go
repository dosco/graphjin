// Code generated from SnowflakeParser.g4 by ANTLR 4.13.2. DO NOT EDIT.

package snowflake // SnowflakeParser
import "github.com/antlr4-go/antlr/v4"

// A complete Visitor for a parse tree produced by SnowflakeParser.
type SnowflakeParserVisitor interface {
	antlr.ParseTreeVisitor

	// Visit a parse tree produced by SnowflakeParser#snowflake_file.
	VisitSnowflake_file(ctx *Snowflake_fileContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#batch.
	VisitBatch(ctx *BatchContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#sql_command.
	VisitSql_command(ctx *Sql_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#ddl_command.
	VisitDdl_command(ctx *Ddl_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#dml_command.
	VisitDml_command(ctx *Dml_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#insert_statement.
	VisitInsert_statement(ctx *Insert_statementContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#insert_multi_table_statement.
	VisitInsert_multi_table_statement(ctx *Insert_multi_table_statementContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#into_clause2.
	VisitInto_clause2(ctx *Into_clause2Context) interface{}

	// Visit a parse tree produced by SnowflakeParser#values_list.
	VisitValues_list(ctx *Values_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#value_item.
	VisitValue_item(ctx *Value_itemContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#merge_statement.
	VisitMerge_statement(ctx *Merge_statementContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#merge_matches.
	VisitMerge_matches(ctx *Merge_matchesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#merge_update_delete.
	VisitMerge_update_delete(ctx *Merge_update_deleteContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#merge_insert.
	VisitMerge_insert(ctx *Merge_insertContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#update_statement.
	VisitUpdate_statement(ctx *Update_statementContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#table_or_query.
	VisitTable_or_query(ctx *Table_or_queryContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#delete_statement.
	VisitDelete_statement(ctx *Delete_statementContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#values_builder.
	VisitValues_builder(ctx *Values_builderContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#other_command.
	VisitOther_command(ctx *Other_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#copy_into_table.
	VisitCopy_into_table(ctx *Copy_into_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#external_location.
	VisitExternal_location(ctx *External_locationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#files.
	VisitFiles(ctx *FilesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#file_format.
	VisitFile_format(ctx *File_formatContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#format_name.
	VisitFormat_name(ctx *Format_nameContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#format_type.
	VisitFormat_type(ctx *Format_typeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#stage_file_format.
	VisitStage_file_format(ctx *Stage_file_formatContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#copy_into_location.
	VisitCopy_into_location(ctx *Copy_into_locationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#comment.
	VisitComment(ctx *CommentContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#commit.
	VisitCommit(ctx *CommitContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#execute_immediate.
	VisitExecute_immediate(ctx *Execute_immediateContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#execute_task.
	VisitExecute_task(ctx *Execute_taskContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#explain.
	VisitExplain(ctx *ExplainContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#parallel.
	VisitParallel(ctx *ParallelContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#get_dml.
	VisitGet_dml(ctx *Get_dmlContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#grant_ownership.
	VisitGrant_ownership(ctx *Grant_ownershipContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#grant_to_role.
	VisitGrant_to_role(ctx *Grant_to_roleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#global_privileges.
	VisitGlobal_privileges(ctx *Global_privilegesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#global_privilege.
	VisitGlobal_privilege(ctx *Global_privilegeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#account_object_privileges.
	VisitAccount_object_privileges(ctx *Account_object_privilegesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#account_object_privilege.
	VisitAccount_object_privilege(ctx *Account_object_privilegeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#schema_privileges.
	VisitSchema_privileges(ctx *Schema_privilegesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#schema_privilege.
	VisitSchema_privilege(ctx *Schema_privilegeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#schema_object_privileges.
	VisitSchema_object_privileges(ctx *Schema_object_privilegesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#schema_object_privilege.
	VisitSchema_object_privilege(ctx *Schema_object_privilegeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#grant_to_share.
	VisitGrant_to_share(ctx *Grant_to_shareContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#object_privilege.
	VisitObject_privilege(ctx *Object_privilegeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#grant_role.
	VisitGrant_role(ctx *Grant_roleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#role_name.
	VisitRole_name(ctx *Role_nameContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#system_defined_role.
	VisitSystem_defined_role(ctx *System_defined_roleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#list.
	VisitList(ctx *ListContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#internal_stage.
	VisitInternal_stage(ctx *Internal_stageContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#external_stage.
	VisitExternal_stage(ctx *External_stageContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#put.
	VisitPut(ctx *PutContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#remove.
	VisitRemove(ctx *RemoveContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#revoke_from_role.
	VisitRevoke_from_role(ctx *Revoke_from_roleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#revoke_from_share.
	VisitRevoke_from_share(ctx *Revoke_from_shareContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#revoke_role.
	VisitRevoke_role(ctx *Revoke_roleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#rollback.
	VisitRollback(ctx *RollbackContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#set.
	VisitSet(ctx *SetContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#truncate_materialized_view.
	VisitTruncate_materialized_view(ctx *Truncate_materialized_viewContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#truncate_table.
	VisitTruncate_table(ctx *Truncate_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#unset.
	VisitUnset(ctx *UnsetContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_command.
	VisitAlter_command(ctx *Alter_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#account_params.
	VisitAccount_params(ctx *Account_paramsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#object_params.
	VisitObject_params(ctx *Object_paramsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#default_ddl_collation.
	VisitDefault_ddl_collation(ctx *Default_ddl_collationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#object_properties.
	VisitObject_properties(ctx *Object_propertiesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#session_params.
	VisitSession_params(ctx *Session_paramsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_account.
	VisitAlter_account(ctx *Alter_accountContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#enabled_true_false.
	VisitEnabled_true_false(ctx *Enabled_true_falseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_alert.
	VisitAlter_alert(ctx *Alter_alertContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#resume_suspend.
	VisitResume_suspend(ctx *Resume_suspendContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alert_set_clause.
	VisitAlert_set_clause(ctx *Alert_set_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alert_unset_clause.
	VisitAlert_unset_clause(ctx *Alert_unset_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_api_integration.
	VisitAlter_api_integration(ctx *Alter_api_integrationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#api_integration_property.
	VisitApi_integration_property(ctx *Api_integration_propertyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_connection.
	VisitAlter_connection(ctx *Alter_connectionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_database.
	VisitAlter_database(ctx *Alter_databaseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#database_property.
	VisitDatabase_property(ctx *Database_propertyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#account_id_list.
	VisitAccount_id_list(ctx *Account_id_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_dynamic_table.
	VisitAlter_dynamic_table(ctx *Alter_dynamic_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_external_table.
	VisitAlter_external_table(ctx *Alter_external_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#ignore_edition_check.
	VisitIgnore_edition_check(ctx *Ignore_edition_checkContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#replication_schedule.
	VisitReplication_schedule(ctx *Replication_scheduleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#db_name_list.
	VisitDb_name_list(ctx *Db_name_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#share_name_list.
	VisitShare_name_list(ctx *Share_name_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#full_acct_list.
	VisitFull_acct_list(ctx *Full_acct_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_failover_group.
	VisitAlter_failover_group(ctx *Alter_failover_groupContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_file_format.
	VisitAlter_file_format(ctx *Alter_file_formatContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_function.
	VisitAlter_function(ctx *Alter_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_function_signature.
	VisitAlter_function_signature(ctx *Alter_function_signatureContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#data_type_list.
	VisitData_type_list(ctx *Data_type_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_masking_policy.
	VisitAlter_masking_policy(ctx *Alter_masking_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_materialized_view.
	VisitAlter_materialized_view(ctx *Alter_materialized_viewContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_network_policy.
	VisitAlter_network_policy(ctx *Alter_network_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_notification_integration.
	VisitAlter_notification_integration(ctx *Alter_notification_integrationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_pipe.
	VisitAlter_pipe(ctx *Alter_pipeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_procedure.
	VisitAlter_procedure(ctx *Alter_procedureContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_replication_group.
	VisitAlter_replication_group(ctx *Alter_replication_groupContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#credit_quota.
	VisitCredit_quota(ctx *Credit_quotaContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#frequency.
	VisitFrequency(ctx *FrequencyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#notify_users.
	VisitNotify_users(ctx *Notify_usersContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#triggerDefinition.
	VisitTriggerDefinition(ctx *TriggerDefinitionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_resource_monitor.
	VisitAlter_resource_monitor(ctx *Alter_resource_monitorContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_role.
	VisitAlter_role(ctx *Alter_roleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_row_access_policy.
	VisitAlter_row_access_policy(ctx *Alter_row_access_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_schema.
	VisitAlter_schema(ctx *Alter_schemaContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#schema_property.
	VisitSchema_property(ctx *Schema_propertyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_security_integration.
	VisitAlter_security_integration(ctx *Alter_security_integrationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_security_integration_external_oauth.
	VisitAlter_security_integration_external_oauth(ctx *Alter_security_integration_external_oauthContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#security_integration_external_oauth_property.
	VisitSecurity_integration_external_oauth_property(ctx *Security_integration_external_oauth_propertyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_security_integration_snowflake_oauth.
	VisitAlter_security_integration_snowflake_oauth(ctx *Alter_security_integration_snowflake_oauthContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#security_integration_snowflake_oauth_property.
	VisitSecurity_integration_snowflake_oauth_property(ctx *Security_integration_snowflake_oauth_propertyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_security_integration_saml2.
	VisitAlter_security_integration_saml2(ctx *Alter_security_integration_saml2Context) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_security_integration_scim.
	VisitAlter_security_integration_scim(ctx *Alter_security_integration_scimContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#security_integration_scim_property.
	VisitSecurity_integration_scim_property(ctx *Security_integration_scim_propertyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_sequence.
	VisitAlter_sequence(ctx *Alter_sequenceContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_session.
	VisitAlter_session(ctx *Alter_sessionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_session_policy.
	VisitAlter_session_policy(ctx *Alter_session_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_share.
	VisitAlter_share(ctx *Alter_shareContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_stage.
	VisitAlter_stage(ctx *Alter_stageContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_storage_integration.
	VisitAlter_storage_integration(ctx *Alter_storage_integrationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_stream.
	VisitAlter_stream(ctx *Alter_streamContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_table.
	VisitAlter_table(ctx *Alter_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#clustering_action.
	VisitClustering_action(ctx *Clustering_actionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#table_column_action.
	VisitTable_column_action(ctx *Table_column_actionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#inline_constraint.
	VisitInline_constraint(ctx *Inline_constraintContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#constraint_properties.
	VisitConstraint_properties(ctx *Constraint_propertiesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#ext_table_column_action.
	VisitExt_table_column_action(ctx *Ext_table_column_actionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#constraint_action.
	VisitConstraint_action(ctx *Constraint_actionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#search_optimization_action.
	VisitSearch_optimization_action(ctx *Search_optimization_actionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#search_method_with_target.
	VisitSearch_method_with_target(ctx *Search_method_with_targetContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_table_alter_column.
	VisitAlter_table_alter_column(ctx *Alter_table_alter_columnContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_column_decl_list.
	VisitAlter_column_decl_list(ctx *Alter_column_decl_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_column_decl.
	VisitAlter_column_decl(ctx *Alter_column_declContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_column_opts.
	VisitAlter_column_opts(ctx *Alter_column_optsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#column_set_tags.
	VisitColumn_set_tags(ctx *Column_set_tagsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#column_unset_tags.
	VisitColumn_unset_tags(ctx *Column_unset_tagsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_tag.
	VisitAlter_tag(ctx *Alter_tagContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_task.
	VisitAlter_task(ctx *Alter_taskContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_user.
	VisitAlter_user(ctx *Alter_userContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_view.
	VisitAlter_view(ctx *Alter_viewContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_modify.
	VisitAlter_modify(ctx *Alter_modifyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_warehouse.
	VisitAlter_warehouse(ctx *Alter_warehouseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_connection_opts.
	VisitAlter_connection_opts(ctx *Alter_connection_optsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_user_opts.
	VisitAlter_user_opts(ctx *Alter_user_optsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_tag_opts.
	VisitAlter_tag_opts(ctx *Alter_tag_optsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_network_policy_opts.
	VisitAlter_network_policy_opts(ctx *Alter_network_policy_optsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_warehouse_opts.
	VisitAlter_warehouse_opts(ctx *Alter_warehouse_optsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alter_account_opts.
	VisitAlter_account_opts(ctx *Alter_account_optsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#set_tags.
	VisitSet_tags(ctx *Set_tagsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#tag_decl_list.
	VisitTag_decl_list(ctx *Tag_decl_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#unset_tags.
	VisitUnset_tags(ctx *Unset_tagsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_command.
	VisitCreate_command(ctx *Create_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_account.
	VisitCreate_account(ctx *Create_accountContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_alert.
	VisitCreate_alert(ctx *Create_alertContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alert_condition.
	VisitAlert_condition(ctx *Alert_conditionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alert_action.
	VisitAlert_action(ctx *Alert_actionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_api_integration.
	VisitCreate_api_integration(ctx *Create_api_integrationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_object_clone.
	VisitCreate_object_clone(ctx *Create_object_cloneContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_connection.
	VisitCreate_connection(ctx *Create_connectionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_database.
	VisitCreate_database(ctx *Create_databaseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_dynamic_table.
	VisitCreate_dynamic_table(ctx *Create_dynamic_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#clone_at_before.
	VisitClone_at_before(ctx *Clone_at_beforeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#at_before1.
	VisitAt_before1(ctx *At_before1Context) interface{}

	// Visit a parse tree produced by SnowflakeParser#header_decl.
	VisitHeader_decl(ctx *Header_declContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#compression_type.
	VisitCompression_type(ctx *Compression_typeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#compression.
	VisitCompression(ctx *CompressionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_external_function.
	VisitCreate_external_function(ctx *Create_external_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_external_table.
	VisitCreate_external_table(ctx *Create_external_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#external_table_column_decl.
	VisitExternal_table_column_decl(ctx *External_table_column_declContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#external_table_column_decl_list.
	VisitExternal_table_column_decl_list(ctx *External_table_column_decl_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#full_acct.
	VisitFull_acct(ctx *Full_acctContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#integration_type_name.
	VisitIntegration_type_name(ctx *Integration_type_nameContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_failover_group.
	VisitCreate_failover_group(ctx *Create_failover_groupContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#type_fileformat.
	VisitType_fileformat(ctx *Type_fileformatContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_file_format.
	VisitCreate_file_format(ctx *Create_file_formatContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#arg_decl.
	VisitArg_decl(ctx *Arg_declContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#col_decl.
	VisitCol_decl(ctx *Col_declContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#function_definition.
	VisitFunction_definition(ctx *Function_definitionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_function.
	VisitCreate_function(ctx *Create_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_managed_account.
	VisitCreate_managed_account(ctx *Create_managed_accountContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_masking_policy.
	VisitCreate_masking_policy(ctx *Create_masking_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#tag_decl.
	VisitTag_decl(ctx *Tag_declContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#column_list_in_parentheses.
	VisitColumn_list_in_parentheses(ctx *Column_list_in_parenthesesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_materialized_view.
	VisitCreate_materialized_view(ctx *Create_materialized_viewContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_network_policy.
	VisitCreate_network_policy(ctx *Create_network_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#cloud_provider_params_auto.
	VisitCloud_provider_params_auto(ctx *Cloud_provider_params_autoContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#cloud_provider_params_push.
	VisitCloud_provider_params_push(ctx *Cloud_provider_params_pushContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_notification_integration.
	VisitCreate_notification_integration(ctx *Create_notification_integrationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_pipe.
	VisitCreate_pipe(ctx *Create_pipeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#caller_owner.
	VisitCaller_owner(ctx *Caller_ownerContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#executa_as.
	VisitExecuta_as(ctx *Executa_asContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#procedure_definition.
	VisitProcedure_definition(ctx *Procedure_definitionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_procedure.
	VisitCreate_procedure(ctx *Create_procedureContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_replication_group.
	VisitCreate_replication_group(ctx *Create_replication_groupContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_resource_monitor.
	VisitCreate_resource_monitor(ctx *Create_resource_monitorContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_role.
	VisitCreate_role(ctx *Create_roleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_row_access_policy.
	VisitCreate_row_access_policy(ctx *Create_row_access_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_schema.
	VisitCreate_schema(ctx *Create_schemaContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_security_integration_external_oauth.
	VisitCreate_security_integration_external_oauth(ctx *Create_security_integration_external_oauthContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#implicit_none.
	VisitImplicit_none(ctx *Implicit_noneContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_security_integration_snowflake_oauth.
	VisitCreate_security_integration_snowflake_oauth(ctx *Create_security_integration_snowflake_oauthContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_security_integration_saml2.
	VisitCreate_security_integration_saml2(ctx *Create_security_integration_saml2Context) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_security_integration_scim.
	VisitCreate_security_integration_scim(ctx *Create_security_integration_scimContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#network_policy.
	VisitNetwork_policy(ctx *Network_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#partner_application.
	VisitPartner_application(ctx *Partner_applicationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#start_with.
	VisitStart_with(ctx *Start_withContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#increment_by.
	VisitIncrement_by(ctx *Increment_byContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_sequence.
	VisitCreate_sequence(ctx *Create_sequenceContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_session_policy.
	VisitCreate_session_policy(ctx *Create_session_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_share.
	VisitCreate_share(ctx *Create_shareContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#character.
	VisitCharacter(ctx *CharacterContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#format_type_options.
	VisitFormat_type_options(ctx *Format_type_optionsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#copy_options.
	VisitCopy_options(ctx *Copy_optionsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#internal_stage_params.
	VisitInternal_stage_params(ctx *Internal_stage_paramsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#stage_type.
	VisitStage_type(ctx *Stage_typeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#stage_master_key.
	VisitStage_master_key(ctx *Stage_master_keyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#stage_kms_key.
	VisitStage_kms_key(ctx *Stage_kms_keyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#stage_encryption_opts_aws.
	VisitStage_encryption_opts_aws(ctx *Stage_encryption_opts_awsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#aws_token.
	VisitAws_token(ctx *Aws_tokenContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#aws_key_id.
	VisitAws_key_id(ctx *Aws_key_idContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#aws_secret_key.
	VisitAws_secret_key(ctx *Aws_secret_keyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#aws_role.
	VisitAws_role(ctx *Aws_roleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#external_stage_params.
	VisitExternal_stage_params(ctx *External_stage_paramsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#true_false.
	VisitTrue_false(ctx *True_falseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#enable.
	VisitEnable(ctx *EnableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#refresh_on_create.
	VisitRefresh_on_create(ctx *Refresh_on_createContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#auto_refresh.
	VisitAuto_refresh(ctx *Auto_refreshContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#notification_integration.
	VisitNotification_integration(ctx *Notification_integrationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#directory_table_params.
	VisitDirectory_table_params(ctx *Directory_table_paramsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_stage.
	VisitCreate_stage(ctx *Create_stageContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#cloud_provider_params.
	VisitCloud_provider_params(ctx *Cloud_provider_paramsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#cloud_provider_params2.
	VisitCloud_provider_params2(ctx *Cloud_provider_params2Context) interface{}

	// Visit a parse tree produced by SnowflakeParser#cloud_provider_params3.
	VisitCloud_provider_params3(ctx *Cloud_provider_params3Context) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_storage_integration.
	VisitCreate_storage_integration(ctx *Create_storage_integrationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#copy_grants.
	VisitCopy_grants(ctx *Copy_grantsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#append_only.
	VisitAppend_only(ctx *Append_onlyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#insert_only.
	VisitInsert_only(ctx *Insert_onlyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_initial_rows.
	VisitShow_initial_rows(ctx *Show_initial_rowsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#stream_time.
	VisitStream_time(ctx *Stream_timeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_stream.
	VisitCreate_stream(ctx *Create_streamContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#temporary.
	VisitTemporary(ctx *TemporaryContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#table_type.
	VisitTable_type(ctx *Table_typeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#with_tags.
	VisitWith_tags(ctx *With_tagsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#with_row_access_policy.
	VisitWith_row_access_policy(ctx *With_row_access_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#cluster_by.
	VisitCluster_by(ctx *Cluster_byContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#change_tracking.
	VisitChange_tracking(ctx *Change_trackingContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#with_masking_policy.
	VisitWith_masking_policy(ctx *With_masking_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#collate.
	VisitCollate(ctx *CollateContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#not_null.
	VisitNot_null(ctx *Not_nullContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#default_value.
	VisitDefault_value(ctx *Default_valueContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#foreign_key.
	VisitForeign_key(ctx *Foreign_keyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#out_of_line_constraint.
	VisitOut_of_line_constraint(ctx *Out_of_line_constraintContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#full_col_decl.
	VisitFull_col_decl(ctx *Full_col_declContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#column_decl_item.
	VisitColumn_decl_item(ctx *Column_decl_itemContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#column_decl_item_list.
	VisitColumn_decl_item_list(ctx *Column_decl_item_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_table.
	VisitCreate_table(ctx *Create_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_table_as_select.
	VisitCreate_table_as_select(ctx *Create_table_as_selectContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_tag.
	VisitCreate_tag(ctx *Create_tagContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#session_parameter.
	VisitSession_parameter(ctx *Session_parameterContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#session_parameter_list.
	VisitSession_parameter_list(ctx *Session_parameter_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#session_parameter_init_list.
	VisitSession_parameter_init_list(ctx *Session_parameter_init_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#session_parameter_init.
	VisitSession_parameter_init(ctx *Session_parameter_initContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_task.
	VisitCreate_task(ctx *Create_taskContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#sql.
	VisitSql(ctx *SqlContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#call.
	VisitCall(ctx *CallContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_user.
	VisitCreate_user(ctx *Create_userContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#view_col.
	VisitView_col(ctx *View_colContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_view.
	VisitCreate_view(ctx *Create_viewContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#create_warehouse.
	VisitCreate_warehouse(ctx *Create_warehouseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#wh_properties.
	VisitWh_properties(ctx *Wh_propertiesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#wh_params.
	VisitWh_params(ctx *Wh_paramsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#trigger_definition.
	VisitTrigger_definition(ctx *Trigger_definitionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#object_type_name.
	VisitObject_type_name(ctx *Object_type_nameContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#object_type_plural.
	VisitObject_type_plural(ctx *Object_type_pluralContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_command.
	VisitDrop_command(ctx *Drop_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_object.
	VisitDrop_object(ctx *Drop_objectContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_alert.
	VisitDrop_alert(ctx *Drop_alertContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_connection.
	VisitDrop_connection(ctx *Drop_connectionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_database.
	VisitDrop_database(ctx *Drop_databaseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_dynamic_table.
	VisitDrop_dynamic_table(ctx *Drop_dynamic_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_external_table.
	VisitDrop_external_table(ctx *Drop_external_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_failover_group.
	VisitDrop_failover_group(ctx *Drop_failover_groupContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_file_format.
	VisitDrop_file_format(ctx *Drop_file_formatContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_function.
	VisitDrop_function(ctx *Drop_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_integration.
	VisitDrop_integration(ctx *Drop_integrationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_managed_account.
	VisitDrop_managed_account(ctx *Drop_managed_accountContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_masking_policy.
	VisitDrop_masking_policy(ctx *Drop_masking_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_materialized_view.
	VisitDrop_materialized_view(ctx *Drop_materialized_viewContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_network_policy.
	VisitDrop_network_policy(ctx *Drop_network_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_pipe.
	VisitDrop_pipe(ctx *Drop_pipeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_procedure.
	VisitDrop_procedure(ctx *Drop_procedureContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_replication_group.
	VisitDrop_replication_group(ctx *Drop_replication_groupContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_resource_monitor.
	VisitDrop_resource_monitor(ctx *Drop_resource_monitorContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_role.
	VisitDrop_role(ctx *Drop_roleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_row_access_policy.
	VisitDrop_row_access_policy(ctx *Drop_row_access_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_schema.
	VisitDrop_schema(ctx *Drop_schemaContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_sequence.
	VisitDrop_sequence(ctx *Drop_sequenceContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_session_policy.
	VisitDrop_session_policy(ctx *Drop_session_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_share.
	VisitDrop_share(ctx *Drop_shareContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_stage.
	VisitDrop_stage(ctx *Drop_stageContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_stream.
	VisitDrop_stream(ctx *Drop_streamContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_table.
	VisitDrop_table(ctx *Drop_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_tag.
	VisitDrop_tag(ctx *Drop_tagContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_task.
	VisitDrop_task(ctx *Drop_taskContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_user.
	VisitDrop_user(ctx *Drop_userContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_view.
	VisitDrop_view(ctx *Drop_viewContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#drop_warehouse.
	VisitDrop_warehouse(ctx *Drop_warehouseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#cascade_restrict.
	VisitCascade_restrict(ctx *Cascade_restrictContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#arg_types.
	VisitArg_types(ctx *Arg_typesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#undrop_command.
	VisitUndrop_command(ctx *Undrop_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#undrop_database.
	VisitUndrop_database(ctx *Undrop_databaseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#undrop_dynamic_table.
	VisitUndrop_dynamic_table(ctx *Undrop_dynamic_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#undrop_schema.
	VisitUndrop_schema(ctx *Undrop_schemaContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#undrop_table.
	VisitUndrop_table(ctx *Undrop_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#undrop_tag.
	VisitUndrop_tag(ctx *Undrop_tagContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#use_command.
	VisitUse_command(ctx *Use_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#use_database.
	VisitUse_database(ctx *Use_databaseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#use_role.
	VisitUse_role(ctx *Use_roleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#use_schema.
	VisitUse_schema(ctx *Use_schemaContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#use_secondary_roles.
	VisitUse_secondary_roles(ctx *Use_secondary_rolesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#use_warehouse.
	VisitUse_warehouse(ctx *Use_warehouseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#comment_clause.
	VisitComment_clause(ctx *Comment_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#if_suspended.
	VisitIf_suspended(ctx *If_suspendedContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#if_exists.
	VisitIf_exists(ctx *If_existsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#if_not_exists.
	VisitIf_not_exists(ctx *If_not_existsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#or_replace.
	VisitOr_replace(ctx *Or_replaceContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe.
	VisitDescribe(ctx *DescribeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_command.
	VisitDescribe_command(ctx *Describe_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_alert.
	VisitDescribe_alert(ctx *Describe_alertContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_database.
	VisitDescribe_database(ctx *Describe_databaseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_dynamic_table.
	VisitDescribe_dynamic_table(ctx *Describe_dynamic_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_external_table.
	VisitDescribe_external_table(ctx *Describe_external_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_file_format.
	VisitDescribe_file_format(ctx *Describe_file_formatContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_function.
	VisitDescribe_function(ctx *Describe_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_integration.
	VisitDescribe_integration(ctx *Describe_integrationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_masking_policy.
	VisitDescribe_masking_policy(ctx *Describe_masking_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_materialized_view.
	VisitDescribe_materialized_view(ctx *Describe_materialized_viewContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_network_policy.
	VisitDescribe_network_policy(ctx *Describe_network_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_pipe.
	VisitDescribe_pipe(ctx *Describe_pipeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_procedure.
	VisitDescribe_procedure(ctx *Describe_procedureContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_result.
	VisitDescribe_result(ctx *Describe_resultContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_row_access_policy.
	VisitDescribe_row_access_policy(ctx *Describe_row_access_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_schema.
	VisitDescribe_schema(ctx *Describe_schemaContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_search_optimization.
	VisitDescribe_search_optimization(ctx *Describe_search_optimizationContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_sequence.
	VisitDescribe_sequence(ctx *Describe_sequenceContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_session_policy.
	VisitDescribe_session_policy(ctx *Describe_session_policyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_share.
	VisitDescribe_share(ctx *Describe_shareContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_stage.
	VisitDescribe_stage(ctx *Describe_stageContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_stream.
	VisitDescribe_stream(ctx *Describe_streamContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_table.
	VisitDescribe_table(ctx *Describe_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_task.
	VisitDescribe_task(ctx *Describe_taskContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_transaction.
	VisitDescribe_transaction(ctx *Describe_transactionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_user.
	VisitDescribe_user(ctx *Describe_userContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_view.
	VisitDescribe_view(ctx *Describe_viewContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#describe_warehouse.
	VisitDescribe_warehouse(ctx *Describe_warehouseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_command.
	VisitShow_command(ctx *Show_commandContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_alerts.
	VisitShow_alerts(ctx *Show_alertsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_columns.
	VisitShow_columns(ctx *Show_columnsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_connections.
	VisitShow_connections(ctx *Show_connectionsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#starts_with.
	VisitStarts_with(ctx *Starts_withContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#limit_rows.
	VisitLimit_rows(ctx *Limit_rowsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_databases.
	VisitShow_databases(ctx *Show_databasesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_databases_in_failover_group.
	VisitShow_databases_in_failover_group(ctx *Show_databases_in_failover_groupContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_databases_in_replication_group.
	VisitShow_databases_in_replication_group(ctx *Show_databases_in_replication_groupContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_delegated_authorizations.
	VisitShow_delegated_authorizations(ctx *Show_delegated_authorizationsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_external_functions.
	VisitShow_external_functions(ctx *Show_external_functionsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_dynamic_tables.
	VisitShow_dynamic_tables(ctx *Show_dynamic_tablesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_external_tables.
	VisitShow_external_tables(ctx *Show_external_tablesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_failover_groups.
	VisitShow_failover_groups(ctx *Show_failover_groupsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_file_formats.
	VisitShow_file_formats(ctx *Show_file_formatsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_functions.
	VisitShow_functions(ctx *Show_functionsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_global_accounts.
	VisitShow_global_accounts(ctx *Show_global_accountsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_grants.
	VisitShow_grants(ctx *Show_grantsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_grants_opts.
	VisitShow_grants_opts(ctx *Show_grants_optsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_integrations.
	VisitShow_integrations(ctx *Show_integrationsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_locks.
	VisitShow_locks(ctx *Show_locksContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_managed_accounts.
	VisitShow_managed_accounts(ctx *Show_managed_accountsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_masking_policies.
	VisitShow_masking_policies(ctx *Show_masking_policiesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#in_obj.
	VisitIn_obj(ctx *In_objContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#in_obj_2.
	VisitIn_obj_2(ctx *In_obj_2Context) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_materialized_views.
	VisitShow_materialized_views(ctx *Show_materialized_viewsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_network_policies.
	VisitShow_network_policies(ctx *Show_network_policiesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_objects.
	VisitShow_objects(ctx *Show_objectsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_organization_accounts.
	VisitShow_organization_accounts(ctx *Show_organization_accountsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#in_for.
	VisitIn_for(ctx *In_forContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_parameters.
	VisitShow_parameters(ctx *Show_parametersContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_pipes.
	VisitShow_pipes(ctx *Show_pipesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_primary_keys.
	VisitShow_primary_keys(ctx *Show_primary_keysContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_procedures.
	VisitShow_procedures(ctx *Show_proceduresContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_regions.
	VisitShow_regions(ctx *Show_regionsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_replication_accounts.
	VisitShow_replication_accounts(ctx *Show_replication_accountsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_replication_databases.
	VisitShow_replication_databases(ctx *Show_replication_databasesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_replication_groups.
	VisitShow_replication_groups(ctx *Show_replication_groupsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_resource_monitors.
	VisitShow_resource_monitors(ctx *Show_resource_monitorsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_roles.
	VisitShow_roles(ctx *Show_rolesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_row_access_policies.
	VisitShow_row_access_policies(ctx *Show_row_access_policiesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_schemas.
	VisitShow_schemas(ctx *Show_schemasContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_sequences.
	VisitShow_sequences(ctx *Show_sequencesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_session_policies.
	VisitShow_session_policies(ctx *Show_session_policiesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_shares.
	VisitShow_shares(ctx *Show_sharesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_shares_in_failover_group.
	VisitShow_shares_in_failover_group(ctx *Show_shares_in_failover_groupContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_shares_in_replication_group.
	VisitShow_shares_in_replication_group(ctx *Show_shares_in_replication_groupContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_stages.
	VisitShow_stages(ctx *Show_stagesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_streams.
	VisitShow_streams(ctx *Show_streamsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_tables.
	VisitShow_tables(ctx *Show_tablesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_tags.
	VisitShow_tags(ctx *Show_tagsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_tasks.
	VisitShow_tasks(ctx *Show_tasksContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_transactions.
	VisitShow_transactions(ctx *Show_transactionsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_user_functions.
	VisitShow_user_functions(ctx *Show_user_functionsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_users.
	VisitShow_users(ctx *Show_usersContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_variables.
	VisitShow_variables(ctx *Show_variablesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_views.
	VisitShow_views(ctx *Show_viewsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#show_warehouses.
	VisitShow_warehouses(ctx *Show_warehousesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#like_pattern.
	VisitLike_pattern(ctx *Like_patternContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#account_identifier.
	VisitAccount_identifier(ctx *Account_identifierContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#schema_name.
	VisitSchema_name(ctx *Schema_nameContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#object_type.
	VisitObject_type(ctx *Object_typeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#object_type_list.
	VisitObject_type_list(ctx *Object_type_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#tag_value.
	VisitTag_value(ctx *Tag_valueContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#arg_data_type.
	VisitArg_data_type(ctx *Arg_data_typeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#arg_name.
	VisitArg_name(ctx *Arg_nameContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#param_name.
	VisitParam_name(ctx *Param_nameContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#region_group_id.
	VisitRegion_group_id(ctx *Region_group_idContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#snowflake_region_id.
	VisitSnowflake_region_id(ctx *Snowflake_region_idContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#string.
	VisitString(ctx *StringContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#string_list.
	VisitString_list(ctx *String_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#id_.
	VisitId_(ctx *Id_Context) interface{}

	// Visit a parse tree produced by SnowflakeParser#keyword.
	VisitKeyword(ctx *KeywordContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#non_reserved_words.
	VisitNon_reserved_words(ctx *Non_reserved_wordsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#builtin_function.
	VisitBuiltin_function(ctx *Builtin_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#list_operator.
	VisitList_operator(ctx *List_operatorContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#binary_builtin_function.
	VisitBinary_builtin_function(ctx *Binary_builtin_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#binary_or_ternary_builtin_function.
	VisitBinary_or_ternary_builtin_function(ctx *Binary_or_ternary_builtin_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#ternary_builtin_function.
	VisitTernary_builtin_function(ctx *Ternary_builtin_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#pattern.
	VisitPattern(ctx *PatternContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#column_name.
	VisitColumn_name(ctx *Column_nameContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#column_list.
	VisitColumn_list(ctx *Column_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#object_name.
	VisitObject_name(ctx *Object_nameContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#num.
	VisitNum(ctx *NumContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#expr_list.
	VisitExpr_list(ctx *Expr_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#expr_list_sorted.
	VisitExpr_list_sorted(ctx *Expr_list_sortedContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#expr.
	VisitExpr(ctx *ExprContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#iff_expr.
	VisitIff_expr(ctx *Iff_exprContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#trim_expression.
	VisitTrim_expression(ctx *Trim_expressionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#try_cast_expr.
	VisitTry_cast_expr(ctx *Try_cast_exprContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#json_literal.
	VisitJson_literal(ctx *Json_literalContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#kv_pair.
	VisitKv_pair(ctx *Kv_pairContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#value.
	VisitValue(ctx *ValueContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#arr_literal.
	VisitArr_literal(ctx *Arr_literalContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#data_type.
	VisitData_type(ctx *Data_typeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#primitive_expression.
	VisitPrimitive_expression(ctx *Primitive_expressionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#order_by_expr.
	VisitOrder_by_expr(ctx *Order_by_exprContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#asc_desc.
	VisitAsc_desc(ctx *Asc_descContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#over_clause.
	VisitOver_clause(ctx *Over_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#function_call.
	VisitFunction_call(ctx *Function_callContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#ranking_windowed_function.
	VisitRanking_windowed_function(ctx *Ranking_windowed_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#aggregate_function.
	VisitAggregate_function(ctx *Aggregate_functionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#literal.
	VisitLiteral(ctx *LiteralContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#sign.
	VisitSign(ctx *SignContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#full_column_name.
	VisitFull_column_name(ctx *Full_column_nameContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#bracket_expression.
	VisitBracket_expression(ctx *Bracket_expressionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#case_expression.
	VisitCase_expression(ctx *Case_expressionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#switch_search_condition_section.
	VisitSwitch_search_condition_section(ctx *Switch_search_condition_sectionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#switch_section.
	VisitSwitch_section(ctx *Switch_sectionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#query_statement.
	VisitQuery_statement(ctx *Query_statementContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#with_expression.
	VisitWith_expression(ctx *With_expressionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#common_table_expression.
	VisitCommon_table_expression(ctx *Common_table_expressionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#anchor_clause.
	VisitAnchor_clause(ctx *Anchor_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#recursive_clause.
	VisitRecursive_clause(ctx *Recursive_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#select_statement.
	VisitSelect_statement(ctx *Select_statementContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#set_operators.
	VisitSet_operators(ctx *Set_operatorsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#select_optional_clauses.
	VisitSelect_optional_clauses(ctx *Select_optional_clausesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#select_clause.
	VisitSelect_clause(ctx *Select_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#select_top_clause.
	VisitSelect_top_clause(ctx *Select_top_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#select_list_no_top.
	VisitSelect_list_no_top(ctx *Select_list_no_topContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#select_list_top.
	VisitSelect_list_top(ctx *Select_list_topContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#select_list.
	VisitSelect_list(ctx *Select_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#select_list_elem.
	VisitSelect_list_elem(ctx *Select_list_elemContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#column_elem.
	VisitColumn_elem(ctx *Column_elemContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#as_alias.
	VisitAs_alias(ctx *As_aliasContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#expression_elem.
	VisitExpression_elem(ctx *Expression_elemContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#column_position.
	VisitColumn_position(ctx *Column_positionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#all_distinct.
	VisitAll_distinct(ctx *All_distinctContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#top_clause.
	VisitTop_clause(ctx *Top_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#into_clause.
	VisitInto_clause(ctx *Into_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#var_list.
	VisitVar_list(ctx *Var_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#var.
	VisitVar(ctx *VarContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#from_clause.
	VisitFrom_clause(ctx *From_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#table_sources.
	VisitTable_sources(ctx *Table_sourcesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#table_source.
	VisitTable_source(ctx *Table_sourceContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#table_source_item_joined.
	VisitTable_source_item_joined(ctx *Table_source_item_joinedContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#object_ref.
	VisitObject_ref(ctx *Object_refContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#flatten_table_option.
	VisitFlatten_table_option(ctx *Flatten_table_optionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#flatten_table.
	VisitFlatten_table(ctx *Flatten_tableContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#prior_list.
	VisitPrior_list(ctx *Prior_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#prior_item.
	VisitPrior_item(ctx *Prior_itemContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#outer_join.
	VisitOuter_join(ctx *Outer_joinContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#join_type.
	VisitJoin_type(ctx *Join_typeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#join_clause.
	VisitJoin_clause(ctx *Join_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#at_before.
	VisitAt_before(ctx *At_beforeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#end.
	VisitEnd(ctx *EndContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#changes.
	VisitChanges(ctx *ChangesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#default_append_only.
	VisitDefault_append_only(ctx *Default_append_onlyContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#partition_by.
	VisitPartition_by(ctx *Partition_byContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#alias.
	VisitAlias(ctx *AliasContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#expr_alias_list.
	VisitExpr_alias_list(ctx *Expr_alias_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#measures.
	VisitMeasures(ctx *MeasuresContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#match_opts.
	VisitMatch_opts(ctx *Match_optsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#row_match.
	VisitRow_match(ctx *Row_matchContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#first_last.
	VisitFirst_last(ctx *First_lastContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#symbol.
	VisitSymbol(ctx *SymbolContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#after_match.
	VisitAfter_match(ctx *After_matchContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#symbol_list.
	VisitSymbol_list(ctx *Symbol_listContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#define.
	VisitDefine(ctx *DefineContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#match_recognize.
	VisitMatch_recognize(ctx *Match_recognizeContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#pivot_unpivot.
	VisitPivot_unpivot(ctx *Pivot_unpivotContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#column_alias_list_in_brackets.
	VisitColumn_alias_list_in_brackets(ctx *Column_alias_list_in_bracketsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#expr_list_in_parentheses.
	VisitExpr_list_in_parentheses(ctx *Expr_list_in_parenthesesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#values.
	VisitValues(ctx *ValuesContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#sample_method.
	VisitSample_method(ctx *Sample_methodContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#repeatable_seed.
	VisitRepeatable_seed(ctx *Repeatable_seedContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#sample_opts.
	VisitSample_opts(ctx *Sample_optsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#sample.
	VisitSample(ctx *SampleContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#search_condition.
	VisitSearch_condition(ctx *Search_conditionContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#comparison_operator.
	VisitComparison_operator(ctx *Comparison_operatorContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#null_not_null.
	VisitNull_not_null(ctx *Null_not_nullContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#subquery.
	VisitSubquery(ctx *SubqueryContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#predicate.
	VisitPredicate(ctx *PredicateContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#where_clause.
	VisitWhere_clause(ctx *Where_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#group_item.
	VisitGroup_item(ctx *Group_itemContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#group_by_clause.
	VisitGroup_by_clause(ctx *Group_by_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#having_clause.
	VisitHaving_clause(ctx *Having_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#qualify_clause.
	VisitQualify_clause(ctx *Qualify_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#order_item.
	VisitOrder_item(ctx *Order_itemContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#order_by_clause.
	VisitOrder_by_clause(ctx *Order_by_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#row_rows.
	VisitRow_rows(ctx *Row_rowsContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#first_next.
	VisitFirst_next(ctx *First_nextContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#limit_clause.
	VisitLimit_clause(ctx *Limit_clauseContext) interface{}

	// Visit a parse tree produced by SnowflakeParser#supplement_non_reserved_words.
	VisitSupplement_non_reserved_words(ctx *Supplement_non_reserved_wordsContext) interface{}
}
